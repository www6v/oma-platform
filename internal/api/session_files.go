package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/workdir"
)

type promoteSandboxFileRequest struct {
	Path         string `json:"path"`
	Filename     string `json:"filename"`
	MediaType    string `json:"media_type"`
	Downloadable *bool  `json:"downloadable"`
}

func (h *sessionHandlers) handlePromoteSandboxFile(
	w http.ResponseWriter,
	req *http.Request,
) {
	if h.files == nil || h.fileBlobs == nil || h.workdirs == nil {
		writeError(w, http.StatusInternalServerError, "files not configured")
		return
	}
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	var body promoteSandboxFileRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	workdirPath := filepath.Join(
		h.workdirs.BaseDir(),
		sess.ID,
	)
	absPath, err := workdir.ResolveSandboxPath(workdirPath, body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot read sandbox path")
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusBadRequest, "Cannot read sandbox path")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filename := body.Filename
	if filename == "" {
		filename = filepath.Base(body.Path)
	}
	if filename == "" {
		filename = "file"
	}
	mediaType := body.MediaType
	if mediaType == "" {
		mediaType = guessMediaType(filename)
	}
	downloadable := true
	if body.Downloadable != nil {
		downloadable = *body.Downloadable
	}
	tenant := tenantID(req)
	fileID := store.NewFileID()
	blobKey, err := h.fileBlobs.Write(tenant, fileID, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sessionID := sess.ID
	row, err := h.files.Insert(req.Context(), store.CreateFileInput{
		ID:           fileID,
		TenantID:     tenant,
		SessionID:    &sessionID,
		Filename:     filename,
		MediaType:    mediaType,
		SizeBytes:    int64(len(data)),
		Downloadable: downloadable,
		BlobKey:      blobKey,
	})
	if err != nil {
		_ = h.fileBlobs.Delete(blobKey)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, fileRowToRecord(*row))
}

func guessMediaType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
