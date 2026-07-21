package gitview

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Commit is one entry in a file's history (R12, acceptance J2).
type Commit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Date      string `json:"date"`
	Subject   string `json:"subject"`
}

// BlameLine attributes one line of a file to the commit that last touched it
// (R12, acceptance J2).
type BlameLine struct {
	Line    int    `json:"line"`
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Content string `json:"content"`
}

// repoPath turns a workspace document ID into a repository-relative path. The
// workspace root may be a subdirectory of the repository, so the prefix is
// prepended.
func (a *Adapter) repoPath(documentID string) string {
	if a.prefix == "" {
		return documentID
	}
	return a.prefix + "/" + documentID
}

// Diff returns the working-tree diff for a document, matching
// `git diff -- <file>` (acceptance J1). A clean or untracked file yields an
// empty diff rather than an error, because "no changes" is a normal state.
func (a *Adapter) Diff(documentID string) (string, error) {
	if !a.Available() {
		return "", ErrUnavailable
	}
	out, err := a.run(context.Background(), "diff", "--no-color", "--", a.repoPath(documentID))
	if err != nil {
		// The file may be untracked, so git diff reports nothing useful; that is
		// an empty diff, not a fault.
		return "", nil
	}
	return string(out), nil
}

// History returns a document's commit history, newest first, without touching
// the repository (acceptance J2 and J3).
func (a *Adapter) History(documentID string) ([]Commit, error) {
	if !a.Available() {
		return nil, ErrUnavailable
	}
	// Unit-separated fields, newline-separated records: a subject is single-line,
	// so a newline delimiter is safe and simpler than NUL parsing.
	const format = "%H%x1f%h%x1f%an%x1f%aI%x1f%s"
	out, err := a.run(context.Background(), "log", "--no-color", "--pretty=format:"+format, "--", a.repoPath(documentID))
	if err != nil {
		return []Commit{}, nil // untracked or no history: an empty list, not an error
	}
	var commits []Commit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		if len(f) != 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash: f[0], ShortHash: f[1], Author: f[2], Date: f[3], Subject: f[4],
		})
	}
	if commits == nil {
		commits = []Commit{}
	}
	return commits, nil
}

// Blame returns per-line attribution for a document (acceptance J2). It parses
// the line-porcelain format, which repeats each commit's metadata for every
// line and is therefore unambiguous to parse.
func (a *Adapter) Blame(documentID string) ([]BlameLine, error) {
	if !a.Available() {
		return nil, ErrUnavailable
	}
	out, err := a.run(context.Background(), "blame", "--line-porcelain", "--", a.repoPath(documentID))
	if err != nil {
		return []BlameLine{}, nil // uncommitted file: no blame, not an error
	}
	return parseBlame(string(out)), nil
}

// parseBlame reads `git blame --line-porcelain` output into per-line records.
func parseBlame(out string) []BlameLine {
	lines := []BlameLine{}
	var cur BlameLine
	haveHeader := false

	for _, raw := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(raw, "\t"):
			// The content line closes the current group.
			cur.Content = raw[1:]
			if haveHeader {
				lines = append(lines, cur)
			}
			cur = BlameLine{}
			haveHeader = false
		case strings.HasPrefix(raw, "author "):
			cur.Author = strings.TrimPrefix(raw, "author ")
		case strings.HasPrefix(raw, "author-time "):
			cur.Date = unixToISO(strings.TrimPrefix(raw, "author-time "))
		default:
			// A header line: "<hash> <origLine> <finalLine> [<count>]".
			fields := strings.Fields(raw)
			if len(fields) >= 3 && len(fields[0]) >= 7 && isHex(fields[0]) {
				cur.Hash = fields[0]
				if n, err := strconv.Atoi(fields[2]); err == nil {
					cur.Line = n
				}
				haveHeader = true
			}
		}
	}
	return lines
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// unixToISO turns a Unix seconds string into an RFC 3339 UTC date, or returns
// the input unchanged if it cannot be parsed.
func unixToISO(secs string) string {
	n, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return secs
	}
	return time.Unix(n, 0).UTC().Format(time.RFC3339)
}
