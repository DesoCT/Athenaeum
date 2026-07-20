//go:build unix

package security

import "golang.org/x/sys/unix"

// makeFIFO creates a named pipe, used only by tests that assert irregular files
// are rejected.
func makeFIFO(path string) error {
	return unix.Mkfifo(path, 0o644)
}
