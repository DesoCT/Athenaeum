package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newService(t *testing.T) (svc *Service, workspace, personal string) {
	t.Helper()
	workspace = t.TempDir()
	personal = t.TempDir()
	svc = NewService(Options{
		PersonalDir: filepath.Join(personal, "notes"),
		SharedDir:   filepath.Join(workspace, ".athenaeum", "shared", "notes"),
	})
	return svc, workspace, personal
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	n := &Note{
		ID:         "01ABC",
		Title:      "Design review",
		Visibility: VisibilityShared,
		CreatedAt:  "2026-07-21T12:00:00Z",
		UpdatedAt:  "2026-07-21T12:00:00Z",
		Links:      []Link{{Document: "docs/architecture.md", Heading: "Search"}},
		Body:       "Notes here.\n\nSecond paragraph.\n",
	}
	data, err := encode(n)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("note does not start with a YAML fence:\n%s", data)
	}
	got, err := decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Title != n.Title || got.Body != n.Body {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if len(got.Links) != 1 || got.Links[0].Heading != "Search" {
		t.Fatalf("round trip lost links: %+v", got.Links)
	}
}

func TestDecodeRejectsMissingFrontMatter(t *testing.T) {
	if _, err := decode([]byte("just a body, no front matter\n")); err == nil {
		t.Fatal("a note with no front matter was accepted")
	}
}

// TestSharedNoteLocationAndPersistence covers R9 and the G2 storage rule for
// notes: a shared note lands under .athenaeum/shared/notes and reads back.
func TestSharedNoteLocationAndPersistence(t *testing.T) {
	svc, workspace, _ := newService(t)
	created, err := svc.Create(CreateRequest{
		Title:      "Portable",
		Visibility: VisibilityShared,
		Body:       "shared body",
		Links:      []Link{{Document: "docs/a.md", Heading: "Intro"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	want := filepath.Join(workspace, ".athenaeum", "shared", "notes", created.ID+".md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("shared note not at %s: %v", want, err)
	}

	got, err := svc.Read(VisibilityShared, created.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Title != "Portable" || got.Links[0].Heading != "Intro" {
		t.Fatalf("read back wrong note: %+v", got)
	}
}

// TestPersonalNoteStaysOutsideWorkspace applies the G1 rule to notes.
func TestPersonalNoteStaysOutsideWorkspace(t *testing.T) {
	svc, workspace, personal := newService(t)
	if _, err := svc.Create(CreateRequest{Title: "Private", Visibility: VisibilityPersonal, Body: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n := countFiles(t, workspace); n != 0 {
		t.Fatalf("personal note wrote %d files under the workspace; want 0", n)
	}
	if countFiles(t, personal) == 0 {
		t.Fatal("personal note wrote nothing to the user data directory")
	}
}

func TestListNewestFirstAcrossStores(t *testing.T) {
	svc, _, _ := newService(t)
	a, _ := svc.Create(CreateRequest{Title: "Older", Visibility: VisibilityShared, Body: "a"})
	// Force a later timestamp on the second note.
	svc.now = later
	b, _ := svc.Create(CreateRequest{Title: "Newer", Visibility: VisibilityPersonal, Body: "b"})

	list, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list count = %d, want 2", len(list))
	}
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("list not newest-first: %+v", list)
	}
}

func TestUpdateVersionConflict(t *testing.T) {
	svc, _, _ := newService(t)
	created, _ := svc.Create(CreateRequest{Title: "T", Visibility: VisibilityShared, Body: "one"})

	body := "two"
	if _, err := svc.Update(UpdateRequest{
		ID: created.ID, Visibility: VisibilityShared, Body: &body, ExpectedVersion: "sha256:stale",
	}); err == nil {
		t.Fatal("stale update was accepted")
	}

	// The real version succeeds.
	updated, err := svc.Update(UpdateRequest{
		ID: created.ID, Visibility: VisibilityShared, Body: &body, ExpectedVersion: created.Version,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if strings.TrimSpace(updated.Body) != "two" {
		t.Fatalf("update body = %q", updated.Body)
	}
}

func TestDeleteAndUnavailable(t *testing.T) {
	svc, _, _ := newService(t)
	created, _ := svc.Create(CreateRequest{Title: "T", Visibility: VisibilityShared, Body: "x"})
	if err := svc.Delete(VisibilityShared, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Read(VisibilityShared, created.ID); err == nil {
		t.Fatal("deleted note still readable")
	}

	off := NewService(Options{SharedDir: t.TempDir()}) // no personal dir
	_, err := off.Create(CreateRequest{Title: "T", Visibility: VisibilityPersonal, Body: "x"})
	var un *UnavailableError
	if !asType(err, &un) {
		t.Fatalf("error = %v, want *UnavailableError", err)
	}
}

func TestNoteIDTraversalRejected(t *testing.T) {
	svc, _, _ := newService(t)
	if _, err := svc.Read(VisibilityShared, "../../etc/passwd"); err == nil {
		t.Fatal("traversal id was accepted")
	}
}

// helpers

// later is a fixed clock in 2100, so a note created with it always sorts after
// one created with the real time.Now.
func later() time.Time { return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) }

func countFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func asType[T error](err error, target *T) bool {
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
