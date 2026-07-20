// Package assets writes pasted and dropped files into the managed asset
// directory (spec 02 section 3.9, requirement R11).
package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"athenaeum/internal/workspace"
)

// Error codes (requirement N6).
const (
	CodeCollision       = "ASSET_COLLISION"
	CodeUnsupported     = "ASSET_UNSUPPORTED_TYPE"
	CodeTooLarge        = "ASSET_TOO_LARGE"
	CodeOutsideBoundary = "ASSET_OUTSIDE_BOUNDARY"
	CodeWriteFailed     = "ASSET_WRITE_FAILED"
)

// Error reports a failed asset write.
type Error struct {
	Code    string
	Message string
	// Suggestion is a non-colliding name the client may offer (I2).
	Suggestion string
	Err        error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// AsError unwraps an asset error, mirroring errors.As for the concrete type.
func AsError(err error, target **Error) bool {
	return errors.As(err, target)
}

// CodeOf returns the stable code for an asset error, or "".
func CodeOf(err error) string {
	var assetErr *Error
	if errors.As(err, &assetErr) {
		return assetErr.Code
	}
	return ""
}

// maxAssetBytes bounds a single asset.
const maxAssetBytes = 32 << 20

// allowedExtensions is the set an asset may use. Markdown embeds images, so
// the list is deliberately narrow: an arbitrary upload endpoint inside a
// local-first tool is a liability, not a feature.
var allowedExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".svg":  "image/svg+xml",
}

// Request describes an asset to store.
type Request struct {
	// DocumentID is the document the asset is being inserted into. The
	// returned Markdown link is relative to it.
	DocumentID string
	// FileName is the original name, used for its extension and as the basis
	// of a readable slug.
	FileName string
	// Content is the raw bytes.
	Content []byte
	// Overwrite permits replacing an existing file. The client sets it only
	// after the user chose to overwrite (I2).
	Overwrite bool
	// PreferredName, when set, replaces the generated name. Used when the user
	// answers a collision prompt with a new name.
	PreferredName string
}

// Result describes a stored asset.
type Result struct {
	// AssetID is the workspace-relative path of the stored file.
	AssetID string `json:"asset_id"`
	// Markdown is the reference to insert, relative to the document.
	Markdown string `json:"markdown"`
	// RelativePath is the link target used inside Markdown.
	RelativePath string `json:"relative_path"`
	Size         int64  `json:"size"`
}

// Service stores assets for a workspace.
type Service struct {
	ws *workspace.Workspace
}

// New returns an asset service bound to a workspace.
func New(ws *workspace.Workspace) *Service {
	return &Service{ws: ws}
}

// Store writes an asset and returns the Markdown reference to insert.
//
// A collision is detected before anything is written and reported with a
// suggested alternative. Silent overwrite is prohibited by R11 and I2.
func (s *Service) Store(req Request) (*Result, error) {
	if len(req.Content) == 0 {
		return nil, &Error{Code: CodeWriteFailed, Message: "The asset is empty."}
	}
	if len(req.Content) > maxAssetBytes {
		return nil, &Error{
			Code:    CodeTooLarge,
			Message: fmt.Sprintf("The asset is %d bytes, above the %d byte limit.", len(req.Content), maxAssetBytes),
		}
	}

	ext := strings.ToLower(filepath.Ext(req.FileName))
	if _, ok := allowedExtensions[ext]; !ok {
		return nil, &Error{
			Code: CodeUnsupported,
			Message: fmt.Sprintf("%q is not a supported asset type. Athenaeum stores images only.",
				strings.TrimPrefix(ext, ".")),
		}
	}

	cfg := s.ws.Config()
	dir := strings.Trim(cfg.Assets.Directory, "/")
	if dir == "" {
		dir = "assets"
	}

	name := req.PreferredName
	if name == "" {
		name = generateName(cfg.Assets.PasteNaming, req.FileName, ext, req.Content)
	} else {
		name = sanitiseName(name, ext)
	}

	assetID := path.Join(dir, name)

	// The asset directory is inside the workspace and must be writable. This
	// resolves and checks the boundary before any bytes are written.
	absDir := filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(dir))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, &Error{
			Code:    CodeWriteFailed,
			Message: "The asset directory could not be created.",
			Err:     err,
		}
	}
	if !s.ws.Writable(assetID) {
		return nil, &Error{
			Code: CodeOutsideBoundary,
			Message: fmt.Sprintf(
				"%s is outside the configured write boundary. Add it to security.writable to store assets there.",
				assetID),
		}
	}

	absPath := filepath.Join(absDir, name)
	// Confirm the resolved target really is inside the asset directory: a
	// crafted preferred name must not escape it.
	if !strings.HasPrefix(filepath.Clean(absPath), filepath.Clean(absDir)+string(filepath.Separator)) {
		return nil, &Error{
			Code:    CodeOutsideBoundary,
			Message: "That asset name resolves outside the asset directory.",
		}
	}

	// Collision detection happens before the write (R11, I2).
	if _, err := os.Stat(absPath); err == nil && !req.Overwrite {
		return nil, &Error{
			Code:       CodeCollision,
			Message:    fmt.Sprintf("%s already exists.", assetID),
			Suggestion: suggestName(absDir, name, ext),
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, &Error{Code: CodeWriteFailed, Message: "The asset location could not be inspected.", Err: err}
	}

	if err := writeAtomic(absPath, req.Content); err != nil {
		return nil, err
	}

	// Refresh so the new asset appears in enumeration where included.
	_ = s.ws.Refresh()

	relative := relativeTo(req.DocumentID, assetID)
	return &Result{
		AssetID:      assetID,
		RelativePath: relative,
		Markdown:     fmt.Sprintf("![%s](%s)", altTextFor(req.FileName), relative),
		Size:         int64(len(req.Content)),
	}, nil
}

// generateName produces the stored file name.
//
// The "date-hash" scheme in spec 05 section 2 keeps pasted assets sorted and
// collision-resistant without depending on the original name, which for a
// clipboard paste is usually meaningless.
func generateName(scheme, original, ext string, content []byte) string {
	switch scheme {
	case "original":
		return sanitiseName(original, ext)
	default: // "date-hash"
		sum := sha256.Sum256(content)
		return fmt.Sprintf("%s-%s%s",
			time.Now().UTC().Format("2006-01-02"),
			hex.EncodeToString(sum[:])[:10],
			ext)
	}
}

// sanitiseName reduces a user-supplied name to a safe, readable file name.
func sanitiseName(name, ext string) string {
	base := strings.TrimSuffix(path.Base(strings.ReplaceAll(name, "\\", "/")), filepath.Ext(name))

	var b strings.Builder
	var lastHyphen bool
	for _, r := range strings.ToLower(base) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	if cleaned == "" {
		cleaned = "asset"
	}
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned + ext
}

// suggestName finds the first free "name-2.png" style alternative.
func suggestName(dir, name, ext string) string {
	stem := strings.TrimSuffix(name, ext)
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, n, ext)
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
	return ""
}

// relativeTo expresses an asset path relative to the document referencing it,
// so the link keeps working if the workspace moves (constitution C2).
func relativeTo(documentID, assetID string) string {
	documentDir := path.Dir(documentID)
	if documentDir == "." || documentDir == "/" {
		return assetID
	}
	rel, err := filepath.Rel(documentDir, assetID)
	if err != nil {
		return assetID
	}
	return filepath.ToSlash(rel)
}

// altTextFor derives readable alt text from the original file name.
func altTextFor(fileName string) string {
	base := strings.TrimSuffix(path.Base(fileName), filepath.Ext(fileName))
	base = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	base = strings.TrimSpace(base)
	if base == "" || strings.HasPrefix(base, "image") {
		return "Pasted image"
	}
	return base
}

// writeAtomic writes an asset via a same-directory temporary file, matching the
// document write policy (spec 03 section 8).
func writeAtomic(target string, content []byte) error {
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".athenaeum-asset-*.tmp")
	if err != nil {
		return &Error{Code: CodeWriteFailed, Message: "A temporary file could not be created.", Err: err}
	}
	tempName := temp.Name()

	if _, err := temp.Write(content); err != nil {
		temp.Close()
		os.Remove(tempName)
		return &Error{Code: CodeWriteFailed, Message: "The asset could not be written.", Err: err}
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(tempName)
		return &Error{Code: CodeWriteFailed, Message: "The asset could not be flushed.", Err: err}
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return &Error{Code: CodeWriteFailed, Message: "The temporary file could not be closed.", Err: err}
	}
	if err := os.Chmod(tempName, 0o644); err != nil {
		_ = err
	}
	if err := os.Rename(tempName, target); err != nil {
		os.Remove(tempName)
		return &Error{Code: CodeWriteFailed, Message: "The asset could not be moved into place.", Err: err}
	}
	return nil
}
