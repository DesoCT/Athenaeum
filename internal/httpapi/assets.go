package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/assets"
)

// maxAssetRequestBytes bounds an asset upload. Base64 inflates by a third, so
// this sits comfortably above the 32 MB the asset service accepts.
const maxAssetRequestBytes = 48 << 20

type assetRequest struct {
	DocumentID string `json:"document_id"`
	FileName   string `json:"file_name"`
	// Content is base64-encoded bytes. JSON cannot carry raw binary, and a
	// multipart endpoint would be a larger surface for no benefit here.
	Content string `json:"content"`
	// Overwrite is set only after the user answered a collision prompt (I2).
	Overwrite bool `json:"overwrite"`
	// PreferredName is set when the user supplied a different name.
	PreferredName string `json:"preferred_name"`
}

// assetConflictResponse reports a collision with a usable alternative.
type assetConflictResponse struct {
	Error      apiErrorBody `json:"error"`
	Suggestion string       `json:"suggestion"`
}

func (s *Server) handleAssetStore(w http.ResponseWriter, r *http.Request) {
	if s.opts.Assets == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}

	var req assetRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAssetRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The asset is larger than this server accepts.")
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The asset request could not be understood.")
		return
	}

	// The document must be one this workspace includes, so an asset cannot be
	// anchored to an arbitrary path.
	if s.opts.Workspace != nil {
		if _, ok := s.opts.Workspace.Lookup(req.DocumentID); !ok {
			s.writeErrorWithDetails(w, r, http.StatusNotFound, "PATH_NOT_FOUND",
				"No such document in this workspace.",
				map[string]string{"document_id": req.DocumentID})
			return
		}
	}

	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The asset content is not valid base64.")
		return
	}

	result, err := s.opts.Assets.Store(assets.Request{
		DocumentID:    req.DocumentID,
		FileName:      req.FileName,
		Content:       content,
		Overwrite:     req.Overwrite,
		PreferredName: req.PreferredName,
	})
	if err != nil {
		s.writeAssetError(w, r, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) writeAssetError(w http.ResponseWriter, r *http.Request, err error) {
	var assetErr *assets.Error
	if !errors.As(err, &assetErr) {
		s.log.Error("store asset", "error", err)
		s.writeError(w, r, http.StatusInternalServerError, assets.CodeWriteFailed,
			"The asset could not be stored.")
		return
	}

	switch assetErr.Code {
	case assets.CodeCollision:
		// 409 with a suggested name so the UI can offer rename or overwrite.
		s.writeJSON(w, http.StatusConflict, assetConflictResponse{
			Error:      apiErrorBody{Code: assetErr.Code, Message: assetErr.Message},
			Suggestion: assetErr.Suggestion,
		})
	case assets.CodeUnsupported, assets.CodeTooLarge:
		s.writeError(w, r, http.StatusBadRequest, assetErr.Code, assetErr.Message)
	case assets.CodeOutsideBoundary:
		s.writeError(w, r, http.StatusForbidden, assetErr.Code, assetErr.Message)
	default:
		s.log.Error("store asset", "code", assetErr.Code, "error", err)
		s.writeError(w, r, http.StatusInternalServerError, assetErr.Code, assetErr.Message)
	}
}
