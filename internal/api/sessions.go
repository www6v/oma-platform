package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type sessionResponse struct {
	ID           string              `json:"id"`
	AgentID      string              `json:"agent_id"`
	Agent        string              `json:"agent"`
	AgentVersion int                 `json:"agent_version"`
	Title        string              `json:"title"`
	Status       store.SessionStatus `json:"status"`
	CreatedAt    int64               `json:"created_at"`
	UpdatedAt    *int64              `json:"updated_at,omitempty"`
}

func formatSession(s *store.Session) sessionResponse {
	return sessionResponse{
		ID:           s.ID,
		AgentID:      s.AgentID,
		Agent:        s.AgentID,
		AgentVersion: s.AgentVersion,
		Title:        s.Title,
		Status:       s.Status,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

type createSessionRequest struct {
	Agent string `json:"agent"`
	Title string `json:"title"`
}

type appendEventsRequest struct {
	Events []json.RawMessage `json:"events"`
}

type sessionHandlers struct {
	sessions *store.SessionRepo
	events   *store.EventRepo
	hub      *stream.Hub
	registry *session.Registry
	workdirs *workdir.Manager
	harness  harness.Client
}

func (h *sessionHandlers) registerMachine(sess *store.Session) {
	h.registry.Register(sess.ID, &session.Machine{
		TenantID:  defaultTenant,
		SessionID: sess.ID,
		Sessions:  h.sessions,
		Events:    h.events,
		Hub:       h.hub,
		Workdirs:  h.workdirs,
		Harness:   h.harness,
	})
}

func mountSessionRoutes(r chi.Router, h *sessionHandlers) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body createSessionRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Agent == "" {
			writeError(w, http.StatusBadRequest, "agent is required")
			return
		}
		sess, err := h.sessions.Create(req.Context(), store.CreateSessionInput{
			TenantID: defaultTenant,
			AgentID:  body.Agent,
			Title:    body.Title,
		})
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		if err == store.ErrArchived {
			writeError(w, http.StatusConflict, "agent archived")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.registerMachine(sess)
		writeJSON(w, http.StatusCreated, formatSession(sess))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		list, err := h.sessions.List(req.Context(), defaultTenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]sessionResponse, 0, len(list))
		for _, s := range list {
			out = append(out, formatSession(s))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, formatSession(sess))
	})

	r.Post("/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if sess.Status == store.SessionStatusArchived {
			writeError(w, http.StatusConflict, "session archived")
			return
		}

		var body appendEventsRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if len(body.Events) == 0 {
			writeError(w, http.StatusBadRequest, "events array is required")
			return
		}
		for _, ev := range body.Events {
			var meta struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(ev, &meta); err != nil {
				writeError(w, http.StatusBadRequest, "invalid event")
				return
			}
			if meta.Type != "user.message" {
				writeError(w, http.StatusBadRequest, "only user.message supported in MVP")
				return
			}
		}

		if err := h.registry.EnqueueUserMessage(
			req.Context(), id, body.Events[0], nil,
		); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	})

	r.Get("/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		limit := 100
		if raw := req.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				limit = n
			}
		}
		afterSeq := 0
		if raw := req.URL.Query().Get("after_seq"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				afterSeq = n
			}
		}
		orderAsc := req.URL.Query().Get("order") != "desc"
		list, err := h.events.ListEvents(req.Context(), id, afterSeq, limit, orderAsc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]json.RawMessage, 0, len(list))
		for _, ev := range list {
			data = append(data, ev.Payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})

	r.Get("/{id}/events/stream", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		if req.URL.Query().Get("replay") == "1" {
			events, err := h.events.ListEvents(req.Context(), id, 0, 10000, true)
			if err == nil {
				for _, ev := range events {
					writeSSE(w, ev.Seq, ev.Payload)
				}
				flusher.Flush()
			}
		}

		ch, unsub := h.hub.Subscribe(id)
		defer unsub()

		ctx := req.Context()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				writeSSE(w, ev.Seq, ev.Payload)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})
}

func writeSSE(w http.ResponseWriter, seq int, payload json.RawMessage) {
	fmt.Fprintf(w, "id: %d\n", seq)
	fmt.Fprintf(w, "data: %s\n\n", payload)
}
