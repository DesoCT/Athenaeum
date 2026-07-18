// Package web exposes the compiled Svelte frontend as an embedded filesystem
// so that a release is a single Go executable (constitution C6, requirement
// N4, acceptance A1).
//
// dist/ is produced by `npm run build` in this directory and is overwritten on
// every frontend build. Only .gitkeep is tracked; the Makefile builds the
// frontend before the Go binary. If dist/index.html is absent the server says
// so explicitly rather than serving a blank page.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the compiled frontend rooted at dist/.
func Assets() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// Built reports whether a real frontend build is embedded in this binary.
func Built() bool {
	sub, err := Assets()
	if err != nil {
		return false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return false
	}
	return true
}
