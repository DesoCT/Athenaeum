package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// servableTypes maps an extension to the Content-Type used when serving a
// workspace file.
//
// Spec 03 section 9 requires local files to be served through identifier-based
// API routes rather than unrestricted file:// URLs. The allowlist is what makes
// that route safe: only inert media is servable, so this endpoint cannot be
// turned into a way to read arbitrary workspace content.
var servableTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	// SVG is served as a download rather than inline: an SVG can carry script,
	// and rendering one from the workspace origin would defeat sanitisation.
	".svg": "image/svg+xml",
}

// handleAssetServe streams a workspace file for display in the preview.
func (s *Server) handleAssetServe(w http.ResponseWriter, r *http.Request) {
	if s.opts.Workspace == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}

	id := r.PathValue("id")
	ext := strings.ToLower(filepath.Ext(id))
	contentType, ok := servableTypes[ext]
	if !ok {
		s.writeErrorWithDetails(w, r, http.StatusForbidden, "ASSET_NOT_SERVABLE",
			"Only image files are served through this route.",
			map[string]string{"asset_id": id})
		return
	}

	// The guard applies every containment rule in spec 03 section 6: no
	// absolute paths, no traversal, no symlink escape, regular files only.
	//
	// Deliberately not gated on workspace inclusion: include patterns normally
	// list Markdown only, so requiring inclusion would make every image in the
	// workspace unloadable. Containment plus the extension allowlist is what
	// bounds this route.
	absPath, err := s.opts.Workspace.Guard().ResolveRead(id)
	if err != nil {
		s.writeDocumentError(w, r, id, err)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		s.writeErrorWithDetails(w, r, http.StatusNotFound, "PATH_NOT_FOUND",
			"No such file in this workspace.", map[string]string{"asset_id": id})
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		s.writeErrorWithDetails(w, r, http.StatusNotFound, "PATH_NOT_FOUND",
			"No such file in this workspace.", map[string]string{"asset_id": id})
		return
	}

	w.Header().Set("Content-Type", contentType)
	// nosniff is set globally; this stops an SVG being rendered as a document
	// in its own right even if it is opened directly.
	if ext == ".svg" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	// Workspace files change under the user's hand, so they are revalidated
	// rather than cached: a stale image would contradict what is on disk.
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), file)
}
