package api

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

func (h *sessionHandlers) mountSessionAuxRoutes(r chi.Router) {
	r.Get("/{id}/threads", h.handleSessionThreads)
	r.Get("/{id}/pending", h.handleSessionPending)
	r.Get("/{id}/trajectory", h.handleSessionTrajectory)
	r.Get("/{id}/outputs", h.handleSessionOutputs)
	r.Get("/{id}/outputs/{filename}", h.handleSessionOutputDownload)
}

func (h *sessionHandlers) requireSession(
	w http.ResponseWriter,
	req *http.Request,
) (*store.Session, bool) {
	id := chi.URLParam(req, "id")
	sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return sess, true
}

func (h *sessionHandlers) handleSessionThreads(
	w http.ResponseWriter,
	req *http.Request,
) {
	if _, ok := h.requireSession(w, req); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{
				"id":                "sthr_primary",
				"parent_thread_id":  nil,
				"session_thread_id": "sthr_primary",
			},
		},
	})
}

func (h *sessionHandlers) handleSessionPending(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	items, err := h.listSessionPending(req, sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []pendingListItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *sessionHandlers) handleSessionOutputs(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if h.outputs == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":     []any{},
			"has_more": false,
		})
		return
	}
	files, err := h.outputs.List(tenantID(req), sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		items = append(items, map[string]any{
			"filename":    file.Filename,
			"size_bytes":  file.SizeBytes,
			"uploaded_at": file.UploadedAt,
			"media_type":  file.MediaType,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":     items,
		"has_more": false,
	})
}

func (h *sessionHandlers) handleSessionOutputDownload(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if h.outputs == nil {
		writeError(w, http.StatusNotFound, "Output file not found")
		return
	}
	filename := chi.URLParam(req, "filename")
	if filename == "" ||
		strings.Contains(filename, "..") ||
		strings.ContainsAny(filename, `/\`) {
		writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}
	body, size, mediaType, err := h.outputs.Read(
		tenantID(req), sess.ID, filename,
	)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "Output file not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+filename+`"`,
	)
	_, _ = io.Copy(w, body)
}

func (h *sessionHandlers) handleSessionTrajectory(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}

	events, err := h.events.ListEvents(
		req.Context(), sess.ID, 0, 10000, true,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, buildTrajectory(sess, events))
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
