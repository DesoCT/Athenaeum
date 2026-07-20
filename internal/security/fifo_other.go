//go:build !unix

package security

import "errors"

// makeFIFO is unsupported outside Unix; the calling test skips.
func makeFIFO(string) error {
	return errors.New("named pipes are not supported on this platform")
}
