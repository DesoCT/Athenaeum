// Package atomicfs performs the crash-safe, verified file replacement that spec
// 03 section 8 requires, for any caller that needs it.
//
// It was extracted from internal/documents so that annotation and note sidecars
// (spec 02 sections 3.6 to 3.7 — "use the same atomic-write policy as
// documents") share one implementation rather than a copy that could drift. The
// document write pipeline wraps the errors here into its own stable codes; other
// callers inspect Kind.
//
// This package makes no policy decision about *where* a file may be written.
// The write-boundary check (spec 03 sections 6 to 7) is the caller's job and
// must happen before Write is reached.
package atomicfs

import (
	"bytes"
	"os"
	"path/filepath"
)

// Kind classifies a failure so a caller can map it to its own error vocabulary.
type Kind int

const (
	// KindWriteFailed covers every failure that leaves the target untouched:
	// the temporary file, the write, the flush, or the read-back.
	KindWriteFailed Kind = iota
	// KindNotAtomic means the replace could not be performed atomically. Spec 03
	// section 8 forbids degrading to a truncate-and-write, so this is fatal and
	// the original is left in place.
	KindNotAtomic
	// KindVerifyFailed means the bytes on disk after the replace do not match
	// what was asked for.
	KindVerifyFailed
)

// Error reports a failed atomic write.
type Error struct {
	Kind    Kind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// Write replaces target with payload atomically and verifies the result,
// implementing steps 2 to 8 of spec 03 section 8. The parent directory must
// already exist; the target may or may not.
func Write(target string, payload []byte) error {
	dir := filepath.Dir(target)

	// Preserve the original mode where practical. A file that does not exist yet
	// takes a conservative default.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return &Error{Kind: KindWriteFailed, Message: "the target could not be inspected", Err: err}
	}

	// Step 2: a temporary file in the same directory, therefore the same
	// filesystem, so the rename in step 7 is atomic rather than a copy.
	temp, err := os.CreateTemp(dir, ".athenaeum-*.tmp")
	if err != nil {
		return &Error{Kind: KindWriteFailed, Message: "a temporary file could not be created beside the target", Err: err}
	}
	tempName := temp.Name()

	// Any failure from here on must leave the original untouched and remove the
	// temporary file.
	cleanup := func() {
		temp.Close()
		os.Remove(tempName)
	}

	// Step 3: copy relevant mode bits. Not fatal on filesystems without
	// permission support; the rename still produces a correct file.
	_ = temp.Chmod(mode)

	// Step 4: write the complete content.
	if _, err := temp.Write(payload); err != nil {
		cleanup()
		return &Error{Kind: KindWriteFailed, Message: "the file could not be written", Err: err}
	}

	// Step 5: flush to disk before the rename, so a crash cannot leave the
	// target pointing at a file whose contents were never persisted.
	if err := temp.Sync(); err != nil {
		cleanup()
		return &Error{Kind: KindWriteFailed, Message: "the file could not be flushed to disk", Err: err}
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return &Error{Kind: KindWriteFailed, Message: "the temporary file could not be closed", Err: err}
	}

	// Step 7: atomic replace.
	if err := os.Rename(tempName, target); err != nil {
		os.Remove(tempName)
		return &Error{Kind: KindNotAtomic, Message: "the file could not be replaced atomically, so it was left unchanged", Err: err}
	}

	// Step 6, applied after the rename: fsync the directory so the rename itself
	// is durable. Not supported everywhere, so a failure is tolerated.
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		handle.Close()
	}

	// Step 8: verify the bytes that actually landed.
	written, err := os.ReadFile(target)
	if err != nil {
		return &Error{Kind: KindVerifyFailed, Message: "the file was written but could not be read back for verification", Err: err}
	}
	if !bytes.Equal(written, payload) {
		return &Error{Kind: KindVerifyFailed, Message: "the file on disk does not match what was written"}
	}

	return nil
}
