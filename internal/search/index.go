package search

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"athenaeum/internal/documents"
)

// schemaVersion guards the on-disk projection. Bumping it discards the cache
// and rebuilds, which is always safe: nothing here is authoritative (C2, D-014).
const schemaVersion = 1

// IndexFileName is the projection's file name inside the workspace cache
// directory (spec 03 section 2.3).
const IndexFileName = "search.sqlite"

// readerPoolSize is the "small reader pool" of spec 02 section 6. Queries are
// short and the writer is separate, so a handful of connections is ample.
const readerPoolSize = 4

// Index is the disposable SQLite FTS projection.
//
// Two handles rather than one: spec 02 section 6 requires a single controlled
// writer and permits a small reader pool. SQLite in WAL mode allows readers to
// proceed while the writer holds its transaction, so indexing never blocks a
// query (requirement N2).
type Index struct {
	path   string
	writer *sql.DB
	reader *sql.DB
}

// Open opens or creates the projection under dir.
//
// projectionKey fingerprints everything that changes the meaning of the index —
// the workspace root, the include and exclude patterns, and the search options.
// When it differs from the stored key the projection is discarded rather than
// patched, because a changed include set makes existing rows unverifiable.
func Open(dir, projectionKey string) (*Index, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the search cache directory: %w", err)
	}
	path := filepath.Join(dir, IndexFileName)

	// A first attempt on whatever is already there. Anything wrong with it —
	// a truncated file, a foreign file, an older schema, a different projection
	// key — is answered the same way: throw it away and build again. That is
	// only ever available because the index is a cache (C2, D-014), and it is
	// what makes acceptance F3 hold by construction rather than by care.
	idx, err := openAt(path)
	if err == nil {
		stale, schemaErr := idx.ensureSchema(projectionKey)
		if schemaErr == nil && !stale {
			return idx, nil
		}
		idx.Close()
	}

	if err := removeIndexFiles(path); err != nil {
		return nil, err
	}
	idx, err = openAt(path)
	if err != nil {
		return nil, err
	}
	if _, err := idx.ensureSchema(projectionKey); err != nil {
		idx.Close()
		return nil, err
	}
	return idx, nil
}

func openAt(path string) (*Index, error) {
	// WAL keeps readers running during a write transaction; NORMAL synchronous
	// is right for a cache that can always be rebuilt. busy_timeout stops a
	// concurrent statement failing outright when the writer holds the lock.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open the search index: %w", err)
	}
	writer.SetMaxOpenConns(1)

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open the search index for reading: %w", err)
	}
	reader.SetMaxOpenConns(readerPoolSize)

	if err := writer.Ping(); err != nil {
		writer.Close()
		reader.Close()
		return nil, fmt.Errorf("open the search index: %w", err)
	}
	return &Index{path: path, writer: writer, reader: reader}, nil
}

// removeIndexFiles deletes the database and its WAL sidecars.
func removeIndexFiles(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("discard the stale search index: %w", err)
		}
	}
	return nil
}

// Path returns the projection's file path.
func (i *Index) Path() string { return i.path }

// Close releases both handles.
func (i *Index) Close() error {
	var errs []error
	if i.reader != nil {
		errs = append(errs, i.reader.Close())
	}
	if i.writer != nil {
		errs = append(errs, i.writer.Close())
	}
	return errors.Join(errs...)
}

// schema is the whole projection. Columns are chosen by spec 02 section 3.5:
// path, title, headings, body, tags, and document group.
//
// `documents` carries the filterable metadata and `documents_fts` the searchable
// text, joined on rowid. Group membership is a filter rather than a search
// column, so a group name cannot silently rank documents that never mention it.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
	rowid      INTEGER PRIMARY KEY,
	id         TEXT NOT NULL UNIQUE,
	title      TEXT NOT NULL,
	groups     TEXT NOT NULL DEFAULT '',
	version    TEXT NOT NULL,
	size       INTEGER NOT NULL,
	mod_time   TEXT NOT NULL,
	outline    TEXT NOT NULL DEFAULT '[]',
	indexed_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
	path,
	title,
	headings,
	body,
	tags,
	tokenize='porter unicode61'
);
`

// ensureSchema creates the tables and validates the stored keys.
//
// It reports stale=true when the projection must be discarded and rebuilt.
func (i *Index) ensureSchema(projectionKey string) (stale bool, err error) {
	if _, err := i.writer.Exec(schema); err != nil {
		// A corrupt or foreign file lands here. It is a cache, so treat it as
		// stale rather than failing the launch.
		return true, nil
	}

	storedSchema, err := i.meta("schema_version")
	if err != nil {
		return true, nil
	}
	storedKey, err := i.meta("projection_key")
	if err != nil {
		return true, nil
	}

	want := fmt.Sprint(schemaVersion)
	if storedSchema == "" && storedKey == "" {
		// A brand-new projection: stamp it.
		if err := i.setMeta("schema_version", want); err != nil {
			return false, err
		}
		if err := i.setMeta("projection_key", projectionKey); err != nil {
			return false, err
		}
		return false, nil
	}
	if storedSchema != want || storedKey != projectionKey {
		return true, nil
	}
	return false, nil
}

func (i *Index) meta(key string) (string, error) {
	var value string
	err := i.writer.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (i *Index) setMeta(key, value string) error {
	_, err := i.writer.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Stored is the index's record of one document, used to decide whether a
// reindex is needed.
type Stored struct {
	Version string
	Size    int64
	ModTime string
}

// Snapshot returns every indexed document with the metadata needed to decide
// whether it must be read again.
//
// Size and modification time are compared before the file is opened. That is
// what makes a warm start cheap (requirement N1): an unchanged workspace of
// 5,000 documents is a single query and a map comparison, with no file IO at
// all. The content fingerprint is the fallback check once a file has been read.
func (i *Index) Snapshot() (map[string]Stored, error) {
	rows, err := i.reader.Query(`SELECT id, version, size, mod_time FROM documents`)
	if err != nil {
		return nil, fmt.Errorf("read the search index: %w", err)
	}
	defer rows.Close()

	stored := make(map[string]Stored)
	for rows.Next() {
		var id string
		var entry Stored
		if err := rows.Scan(&id, &entry.Version, &entry.Size, &entry.ModTime); err != nil {
			return nil, fmt.Errorf("read the search index: %w", err)
		}
		stored[id] = entry
	}
	return stored, rows.Err()
}

// Count returns the number of indexed documents.
func (i *Index) Count() (int, error) {
	var count int
	err := i.reader.QueryRow(`SELECT count(*) FROM documents`).Scan(&count)
	return count, err
}

// Put inserts or replaces one document's projection.
func (i *Index) Put(view *documents.IndexView) error {
	return i.PutBatch([]*documents.IndexView{view})
}

// PutBatch writes a set of documents in one transaction.
//
// Batching matters at the N3 scale target: 5,000 individual transactions are
// dominated by commit overhead, and the pure-Go driver is slower than the CGO
// one under write load. Writes go through the single-connection writer handle,
// which is the "one controlled writer" of spec 02 section 6.
func (i *Index) PutBatch(views []*documents.IndexView) error {
	if len(views) == 0 {
		return nil
	}
	tx, err := i.writer.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, view := range views {
		if err := deleteWithin(tx, view.ID); err != nil {
			return err
		}
		outline, err := json.Marshal(view.Headings)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`INSERT INTO documents (id, title, groups, version, size, mod_time, outline, indexed_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			view.ID, view.Title, strings.Join(view.Groups, " "), view.Version,
			view.Size, view.ModTime, string(outline), now)
		if err != nil {
			return err
		}
		rowID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO documents_fts (rowid, path, title, headings, body, tags)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			rowID, pathText(view.ID), view.Title, headingText(view.Headings),
			view.Body, strings.Join(view.Tags, " ")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes a document from the projection.
func (i *Index) Delete(id string) error {
	tx, err := i.writer.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := deleteWithin(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteBatch removes several documents in one transaction.
func (i *Index) DeleteBatch(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := i.writer.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, id := range ids {
		if err := deleteWithin(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// deleteWithin removes a document and its FTS row inside an open transaction.
//
// FTS5 has no foreign keys, so the two deletes are explicit and must stay in
// the same transaction or a crash could orphan a searchable row whose metadata
// is gone — which would return a result the caller cannot open.
func deleteWithin(tx *sql.Tx, id string) error {
	var rowID int64
	err := tx.QueryRow(`SELECT rowid FROM documents WHERE id = ?`, id).Scan(&rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, rowID); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM documents WHERE rowid = ?`, rowID)
	return err
}

// pathText makes a document ID tokenisable.
//
// "docs/spec/02-SYSTEM-ARCHITECTURE.md" tokenises poorly as one string, so
// separators become spaces and the extension is kept as its own token. The
// original ID is still stored verbatim in `documents.id`.
func pathText(id string) string {
	replaced := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ").Replace(id)
	return id + " " + replaced
}

// headingText joins the outline into one searchable field.
func headingText(headings []documents.Heading) string {
	if len(headings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(headings))
	for _, h := range headings {
		parts = append(parts, h.Text)
	}
	return strings.Join(parts, "\n")
}
