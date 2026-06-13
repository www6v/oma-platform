package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type eventListItem struct {
	Seq  int             `json:"seq"`
	Type string          `json:"type"`
	Ts   string          `json:"ts"`
	Data json.RawMessage `json:"data"`
}

type createSessionRequest struct {
	Agent         json.RawMessage `json:"agent"`
	Title         string          `json:"title"`
	EnvironmentID string          `json:"environment_id"`
	Environment   string          `json:"environment"`
}

type appendEventsRequest struct {
	Events []json.RawMessage `json:"events"`
}

type sessionHandlers struct {
	sessions     *store.SessionRepo
	agents       *store.AgentRepo
	events       *store.EventRepo
	pending      *store.PendingRepo
	hub          *stream.Hub
	registry     *session.Registry
	workdirs     *workdir.Manager
	outputs      *sessionoutputs.Store
	harness      harness.Client
	models       *modelresolve.Resolver
	resources    *harness.ResourceResolver
	mcpProxyBase string
	mcpProxyKey  string
	outboundProxyAddr string
	outboundProxyKey  string
}

func (h *sessionHandlers) registerMachine(sess *store.Session) {
	h.registry.Register(sess.ID, &session.Machine{
		TenantID:      sess.TenantID,
		SessionID:     sess.ID,
		Sessions:      h.sessions,
		Agents:        h.agents,
		Events:        h.events,
		Pending:       h.pending,
		Hub:           h.hub,
		Workdirs:      h.workdirs,
		Harness:       h.harness,
		Models:        h.models,
		Resources:     h.resources,
		McpProxyBase:  h.mcpProxyBase,
		McpProxyAPIKey: h.mcpProxyKey,
		OutboundProxyAddr: h.outboundProxyAddr,
		OutboundProxyAPIKey: h.outboundProxyKey,
	})
}

func mountSessionRoutes(r chi.Router, h *sessionHandlers) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body createSessionRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Agent == nil {
			writeError(w, http.StatusBadRequest, "agent is required")
			return
		}
		agentID, err := parseSessionAgentRef(body.Agent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		envID := body.EnvironmentID
		if envID == "" {
			envID = body.Environment
		}
		sess, err := h.sessions.Create(req.Context(), store.CreateSessionInput{
			TenantID:      tenantID(req),
			AgentID:       agentID,
			Title:         body.Title,
			EnvironmentID: envID,
		})
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
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
		writeJSON(w, http.StatusCreated, formatAPISession(sess))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		params := parseSessionListParams(req)
		if params.Err != "" {
			writeError(w, http.StatusBadRequest, params.Err)
			return
		}
		page, err := h.sessions.ListPage(req.Context(), params.Query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(page.Items))
		for _, s := range page.Items {
			out = append(out, formatAPISession(s))
		}
		writeListPage(w, out, page.NextCursor)
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, formatAPISession(sess))
	})

	r.Post("/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
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
		h.registerMachine(sess)

		var body appendEventsRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if len(body.Events) == 0 {
			writeError(w, http.StatusBadRequest, "events array is required")
			return
		}
		runTurn := false
		hasInterrupt := false
		for _, ev := range body.Events {
			var meta struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(ev, &meta); err != nil {
				writeError(w, http.StatusBadRequest, "invalid event")
				return
			}
			if !isAllowedClientEventType(meta.Type) {
				writeError(w, http.StatusBadRequest, "invalid event type")
				return
			}
			if isInterruptEventType(meta.Type) {
				hasInterrupt = true
			}
			if isTurnTriggerEventType(meta.Type) {
				runTurn = true
			}
		}
		if hasInterrupt {
			runTurn = false
		}

		if err := h.registry.EnqueueEvents(
			req.Context(), id, body.Events, runTurn, hasInterrupt, nil,
		); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	})

	r.Post("/{id}/archive", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Archive(req.Context(), tenantID(req), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.registry.Remove(id)
		writeJSON(w, http.StatusOK, formatAPISession(sess))
	})

	r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.registry.Remove(id)
		if err := h.sessions.Delete(req.Context(), tenantID(req), id); err != nil {
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "session_deleted",
			"id":   id,
		})
	})

	r.Get("/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
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
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		afterSeq := parseAfterSeq(req)
		if req.URL.Query().Get("order") == "desc" {
			list, err := h.events.ListEvents(
				req.Context(), id, afterSeq, limit+1, false,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeEventsPage(w, list, limit)
			return
		}
		page, err := h.events.ListEventsPage(req.Context(), id, afterSeq, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]eventListItem, 0, len(page.Items))
		for _, ev := range page.Items {
			data = append(data, formatEventListItem(ev))
		}
		resp := map[string]any{
			"data":     data,
			"has_more": page.HasMore,
		}
		if page.HasMore && page.LastSeq > 0 {
			resp["next_page"] = fmt.Sprintf("seq_%d", page.LastSeq)
		} else {
			resp["next_page"] = nil
		}
		writeJSON(w, http.StatusOK, resp)
	})

	h.mountSessionAuxRoutes(r)

	r.Get("/{id}/events/stream", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		sess, err := h.sessions.Get(req.Context(), tenantID(req), id)
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

func parseAfterSeq(req *http.Request) int {
	if raw := req.URL.Query().Get("after_seq"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	if raw := req.URL.Query().Get("next_page"); raw != "" {
		var seq int
		if _, err := fmt.Sscanf(raw, "seq_%d", &seq); err == nil && seq >= 0 {
			return seq
		}
	}
	return 0
}

func formatEventListItem(ev store.StoredEvent) eventListItem {
	return eventListItem{
		Seq:  ev.Seq,
		Type: ev.Type,
		Ts:   formatISO(ev.CreatedAt),
		Data: ev.Payload,
	}
}

func writeEventsPage(
	w http.ResponseWriter,
	list []store.StoredEvent,
	limit int,
) {
	hasMore := len(list) > limit
	if hasMore {
		list = list[:limit]
	}
	data := make([]eventListItem, 0, len(list))
	for _, ev := range list {
		data = append(data, formatEventListItem(ev))
	}
	resp := map[string]any{
		"data":     data,
		"has_more": hasMore,
	}
	if hasMore && len(list) > 0 {
		resp["next_page"] = fmt.Sprintf("seq_%d", list[len(list)-1].Seq)
	} else {
		resp["next_page"] = nil
	}
	writeJSON(w, http.StatusOK, resp)
}
