package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

func (h *sessionHandlers) mountSessionAuxRoutes(r chi.Router) {
	r.Get("/{id}/threads", h.handleSessionThreads)
	r.Get("/{id}/pending", h.handleSessionPending)
	r.Get("/{id}/trajectory", h.handleSessionTrajectory)
	r.Get("/{id}/outputs", h.handleSessionOutputs)
}

func (h *sessionHandlers) requireSession(
	w http.ResponseWriter,
	req *http.Request,
) (*store.Session, bool) {
	id := chi.URLParam(req, "id")
	sess, err := h.sessions.Get(req.Context(), defaultTenant, id)
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
	if _, ok := h.requireSession(w, req); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (h *sessionHandlers) handleSessionOutputs(
	w http.ResponseWriter,
	req *http.Request,
) {
	if _, ok := h.requireSession(w, req); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
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

	outcome := trajectoryOutcome(sess.Status)
	startedAt := time.UnixMilli(sess.CreatedAt).UTC().Format(time.RFC3339Nano)
	endedAt := startedAt
	if sess.UpdatedAt != nil {
		endedAt = time.UnixMilli(*sess.UpdatedAt).UTC().Format(time.RFC3339Nano)
	}
	if sess.Status == store.SessionStatusRunning {
		endedAt = ""
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version": "oma.trajectory.v1",
		"trajectory_id":  fmt.Sprintf("traj_%s", sess.ID),
		"session_id":     sess.ID,
		"agent_config":   map[string]any{},
		"environment_config": map[string]any{},
		"model": map[string]any{
			"id":       "unknown",
			"provider": "oma-platform",
		},
		"started_at": startedAt,
		"ended_at":   nullIfEmpty(endedAt),
		"outcome":    outcome,
		"events":     []any{},
		"summary": map[string]any{
			"num_events":      len(events),
			"num_turns":       0,
			"num_tool_calls":  0,
			"num_tool_errors": 0,
			"num_threads":     1,
			"duration_ms":     0,
			"token_usage": map[string]any{
				"input_tokens":              0,
				"output_tokens":             0,
				"cache_read_input_tokens":   0,
				"cache_creation_input_tokens": 0,
			},
		},
	})
}

func trajectoryOutcome(status store.SessionStatus) string {
	switch status {
	case store.SessionStatusRunning:
		return "running"
	case store.SessionStatusInterrupted:
		return "interrupted"
	default:
		return "success"
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
