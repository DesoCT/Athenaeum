package annotations

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeDocs is a stand-in DocumentSource so store tests need no real workspace.
type fakeDocs struct {
	content  string
	headings []Heading
	err      error
}

func (f fakeDocs) Source(string) (string, []Heading, error) {
	return f.content, f.headings, f.err
}

// newService builds a Service with both storage roots under t.TempDir, so a
// test can assert exactly which tree a write touched (G1, G2).
func newService(t *testing.T, docs DocumentSource) (svc *Service, workspace, personal string) {
	t.Helper()
	workspace = t.TempDir()
	personal = t.TempDir()
	svc = NewService(Options{
		PersonalDir: filepath.Join(personal, "annotations"),
		SharedDir:   filepath.Join(workspace, ".athenaeum", "shared", "annotations"),
		Docs:        docs,
	})
	return svc, workspace, personal
}

func textAnchor() Anchor {
	return Anchor{Type: AnchorText, Exact: "disposable cache", StartLine: 2, EndLine: 2, Prefix: "a ", Suffix: "."}
}

func TestResolveTextAnchorUnchangedFastPath(t *testing.T) {
	content := "# Title\nThe index is a disposable cache.\n"
	a := Anchor{Type: AnchorText, Exact: "disposable cache", StartLine: 2, EndLine: 2, SourceHash: sourceHash(content)}
	state, start, _ := resolveAnchor(a, content, nil)
	if state != StateAnchored || start != 2 {
		t.Fatalf("unchanged doc: got state=%s start=%d, want anchored/2", state, start)
	}
}

func TestResolveTextAnchorRepairsAfterShift(t *testing.T) {
	original := "# Title\nThe index is a disposable cache.\n"
	a := Anchor{Type: AnchorText, Exact: "disposable cache", StartLine: 2, EndLine: 2, SourceHash: sourceHash(original)}
	// Two lines inserted above the quote; the stored line is now wrong.
	edited := "# Title\n\nintro paragraph\n\nThe index is a disposable cache.\n"
	state, start, _ := resolveAnchor(a, edited, nil)
	if state != StateAnchored || start != 5 {
		t.Fatalf("repair: got state=%s start=%d, want anchored/5", state, start)
	}
}

func TestResolveTextAnchorAmbiguousDetaches(t *testing.T) {
	a := Anchor{Type: AnchorText, Exact: "the value", StartLine: 1, EndLine: 1, SourceHash: "sha256:stale"}
	// The quote now appears twice with no distinguishing context.
	content := "the value\nthe value\n"
	state, _, _ := resolveAnchor(a, content, nil)
	if state != StateDetached {
		t.Fatalf("ambiguous repair: got %s, want detached", state)
	}
}

func TestResolveTextAnchorContextDisambiguates(t *testing.T) {
	a := Anchor{Type: AnchorText, Exact: "value", StartLine: 1, EndLine: 1, Prefix: "second ", Suffix: " here", SourceHash: "sha256:stale"}
	content := "first value gone\nthe second value here\n"
	state, start, _ := resolveAnchor(a, content, nil)
	if state != StateAnchored || start != 2 {
		t.Fatalf("context repair: got state=%s start=%d, want anchored/2", state, start)
	}
}

func TestResolveHeadingAnchor(t *testing.T) {
	headings := []Heading{{Path: []string{"System", "Search"}, Line: 12}}
	present := Anchor{Type: AnchorHeading, HeadingPath: []string{"System", "Search"}}
	if state, line, _ := resolveAnchor(present, "body", headings); state != StateAnchored || line != 12 {
		t.Fatalf("present heading: got %s/%d, want anchored/12", state, line)
	}
	gone := Anchor{Type: AnchorHeading, HeadingPath: []string{"System", "Removed"}}
	if state, _, _ := resolveAnchor(gone, "body", headings); state != StateDetached {
		t.Fatalf("missing heading: got %s, want detached", state)
	}
}

// TestPersonalAnnotationCreatesNoRepositoryFile is acceptance G1.
func TestPersonalAnnotationCreatesNoRepositoryFile(t *testing.T) {
	docs := fakeDocs{content: "# Title\nThe index is a disposable cache.\n"}
	svc, workspace, personal := newService(t, docs)

	if _, _, err := svc.Create(CreateRequest{
		DocumentID: "docs/architecture.md",
		Kind:       KindComment,
		Visibility: VisibilityPersonal,
		Body:       "clarify this",
		Anchor:     textAnchor(),
	}); err != nil {
		t.Fatalf("create personal annotation: %v", err)
	}

	if countFiles(t, workspace) != 0 {
		t.Fatalf("personal annotation wrote %d files under the workspace; want 0 (G1)", countFiles(t, workspace))
	}
	if countFiles(t, personal) == 0 {
		t.Fatal("personal annotation wrote nothing to the user data directory")
	}
}

// TestSharedAnnotationPersistsUnderWorkspace is acceptance G2: a shared
// annotation lands under .athenaeum/shared, and a fresh service — standing in
// for a restart or a copied workspace — reads it back.
func TestSharedAnnotationPersistsUnderWorkspace(t *testing.T) {
	docs := fakeDocs{content: "# Title\nThe index is a disposable cache.\n"}
	svc, workspace, _ := newService(t, docs)

	created, rev, err := svc.Create(CreateRequest{
		DocumentID: "docs/architecture.md",
		Kind:       KindComment,
		Visibility: VisibilityShared,
		Body:       "portable note",
		Anchor:     textAnchor(),
	})
	if err != nil {
		t.Fatalf("create shared annotation: %v", err)
	}

	want := filepath.Join(workspace, ".athenaeum", "shared", "annotations", "docs", "architecture.md.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("shared sidecar not at %s: %v", want, err)
	}

	// A brand-new service over the same roots is a stand-in for reopening or
	// copying the workspace to another machine.
	reopened := NewService(Options{
		SharedDir: filepath.Join(workspace, ".athenaeum", "shared", "annotations"),
		Docs:      docs,
	})
	list, err := reopened.List("docs/architecture.md")
	if err != nil {
		t.Fatalf("reopened list: %v", err)
	}
	if list.SharedRevision != rev {
		t.Fatalf("revision after reopen = %d, want %d", list.SharedRevision, rev)
	}
	if len(list.Annotations) != 1 || list.Annotations[0].ID != created.ID {
		t.Fatalf("reopened annotations = %+v, want the one created", list.Annotations)
	}
	if list.Annotations[0].Anchor.State != StateAnchored {
		t.Fatalf("reopened anchor state = %s, want anchored", list.Annotations[0].Anchor.State)
	}
}

func TestListMergesPersonalAndShared(t *testing.T) {
	docs := fakeDocs{content: "# Title\nThe index is a disposable cache.\n"}
	svc, _, _ := newService(t, docs)
	mustCreate(t, svc, VisibilityPersonal, "mine")
	mustCreate(t, svc, VisibilityShared, "ours")

	list, err := svc.List("docs/architecture.md")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Annotations) != 2 {
		t.Fatalf("merged count = %d, want 2", len(list.Annotations))
	}
}

func TestCreateRejectsStaleRevision(t *testing.T) {
	docs := fakeDocs{content: "# Title\nThe index is a disposable cache.\n"}
	svc, _, _ := newService(t, docs)
	mustCreate(t, svc, VisibilityShared, "first")

	_, _, err := svc.Create(CreateRequest{
		DocumentID:       "docs/architecture.md",
		Kind:             KindComment,
		Visibility:       VisibilityShared,
		Body:             "second",
		Anchor:           textAnchor(),
		ExpectedRevision: 0, // stale: the sidecar is already at revision 1
	})
	var conflict *ConflictError
	if !asType(err, &conflict) {
		t.Fatalf("stale create error = %v, want *ConflictError", err)
	}
	if conflict.CurrentRevision != 1 {
		t.Fatalf("conflict revision = %d, want 1", conflict.CurrentRevision)
	}
}

func TestUpdateResolveAndDelete(t *testing.T) {
	docs := fakeDocs{content: "# Title\nThe index is a disposable cache.\n"}
	svc, _, _ := newService(t, docs)
	created, rev, err := svc.Create(CreateRequest{
		DocumentID: "docs/architecture.md",
		Kind:       KindComment,
		Visibility: VisibilityShared,
		Body:       "open",
		Anchor:     textAnchor(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	resolved := StatusResolved
	updated, rev2, err := svc.Update(UpdateRequest{
		DocumentID:       "docs/architecture.md",
		Visibility:       VisibilityShared,
		ID:               created.ID,
		Status:           &resolved,
		ExpectedRevision: rev,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != StatusResolved || rev2 != rev+1 {
		t.Fatalf("update result: status=%s rev=%d", updated.Status, rev2)
	}

	rev3, err := svc.Delete(DeleteRequest{
		DocumentID:       "docs/architecture.md",
		Visibility:       VisibilityShared,
		ID:               created.ID,
		ExpectedRevision: rev2,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ := svc.List("docs/architecture.md")
	if len(list.Annotations) != 0 || list.SharedRevision != rev3 {
		t.Fatalf("after delete: %d annotations at rev %d", len(list.Annotations), list.SharedRevision)
	}
}

func TestPersonalUnavailableIsExplicit(t *testing.T) {
	docs := fakeDocs{content: "body"}
	svc := NewService(Options{SharedDir: t.TempDir(), Docs: docs}) // no personal dir
	_, _, err := svc.Create(CreateRequest{
		DocumentID: "a.md",
		Kind:       KindComment,
		Visibility: VisibilityPersonal,
		Body:       "x",
		Anchor:     Anchor{Type: AnchorDocument},
	})
	var un *UnavailableError
	if !asType(err, &un) {
		t.Fatalf("error = %v, want *UnavailableError", err)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	docs := fakeDocs{content: "body"}
	svc, _, _ := newService(t, docs)
	_, _, err := svc.Create(CreateRequest{
		DocumentID: "../../etc/passwd",
		Kind:       KindComment,
		Visibility: VisibilityShared,
		Body:       "x",
		Anchor:     Anchor{Type: AnchorDocument},
	})
	if err == nil {
		t.Fatal("traversal document id was accepted")
	}
}

// helpers

func mustCreate(t *testing.T, svc *Service, visibility, body string) *Annotation {
	t.Helper()
	list, _ := svc.List("docs/architecture.md")
	rev := list.PersonalRevision
	if visibility == VisibilityShared {
		rev = list.SharedRevision
	}
	ann, _, err := svc.Create(CreateRequest{
		DocumentID:       "docs/architecture.md",
		Kind:             KindComment,
		Visibility:       visibility,
		Body:             body,
		Anchor:           textAnchor(),
		ExpectedRevision: rev,
	})
	if err != nil {
		t.Fatalf("create %s: %v", visibility, err)
	}
	return ann
}

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

// asType is a tiny errors.As wrapper that keeps the table tests readable.
func asType[T error](err error, target *T) bool {
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
