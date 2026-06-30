package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

func (h *sessionHandlers) handleSessionThreadRetrieve(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	threadID := chi.URLParam(req, "thread_id")
	thread, found, err := h.lookupSessionThread(req, sess, threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "Thread not found")
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (h *sessionHandlers) handleSessionThreadArchive(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	threadID := chi.URLParam(req, "thread_id")
	if threadID == primaryThreadID {
		writeError(w, http.StatusBadRequest, "Cannot archive primary thread")
		return
	}
	thread, found, err := h.lookupSessionThread(req, sess, threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "Thread not found")
		return
	}
	if status, _ := thread["status"].(string); status == "archived" {
		writeJSON(w, http.StatusOK, thread)
		return
	}

	now := time.Now().UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"type":              "session.thread_archived",
		"session_thread_id": threadID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := h.events.AppendEvents(
		req.Context(), sess.ID, []json.RawMessage{payload},
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	thread["status"] = "archived"
	thread["archived_at"] = formatISO(now)
	thread["updated_at"] = formatISO(now)
	writeJSON(w, http.StatusOK, thread)
}

func (h *sessionHandlers) handleSessionThreadEvents(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	threadID := chi.URLParam(req, "thread_id")
	if _, found, err := h.lookupSessionThread(req, sess, threadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		writeError(w, http.StatusNotFound, "Thread not found")
		return
	}

	limit := 100
	if raw := req.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	afterSeq := parseAfterSeq(req)
	events, err := h.events.ListEvents(
		req.Context(), sess.ID, afterSeq, 10000, true,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := filterEventsForThread(events, threadID)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	hasMore := len(filtered) == limit
	data := make([]eventListItem, 0, len(filtered))
	for _, ev := range filtered {
		data = append(data, formatEventListItem(ev))
	}
	resp := map[string]any{
		"data":     data,
		"has_more": hasMore,
	}
	if hasMore && len(filtered) > 0 {
		resp["next_page"] = fmt.Sprintf("seq_%d", filtered[len(filtered)-1].Seq)
	} else {
		resp["next_page"] = nil
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *sessionHandlers) handleSessionThreadStream(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	threadID := chi.URLParam(req, "thread_id")
	if _, found, err := h.lookupSessionThread(req, sess, threadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		writeError(w, http.StatusNotFound, "Thread not found")
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
		events, err := h.events.ListEvents(req.Context(), sess.ID, 0, 10000, true)
		if err == nil {
			for _, ev := range filterEventsForThread(events, threadID) {
				writeSSE(w, ev.Seq, ev.Payload)
			}
			flusher.Flush()
		}
	}

	ch, unsub := h.hub.Subscribe(sess.ID)
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
			if !eventMatchesThread(ev.Payload, threadID) {
				continue
			}
			writeSSE(w, ev.Seq, ev.Payload)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (h *sessionHandlers) lookupSessionThread(
	req *http.Request,
	sess *store.Session,
	threadID string,
) (map[string]any, bool, error) {
	includeArchived := req.URL.Query().Get("include_archived") == "true"
	events, err := h.events.ListEvents(
		req.Context(), sess.ID, 0, 10000, true,
	)
	if err != nil {
		return nil, false, err
	}
	threads := deriveSessionThreads(sess, events, includeArchived)
	for _, th := range threads {
		if id, _ := th["id"].(string); id == threadID {
			return th, true, nil
		}
	}
	return nil, false, nil
}

func filterEventsForThread(
	events []store.StoredEvent,
	threadID string,
) []store.StoredEvent {
	out := make([]store.StoredEvent, 0, len(events))
	for _, ev := range events {
		if eventMatchesThread(ev.Payload, threadID) {
			out = append(out, ev)
		}
	}
	return out
}

func eventMatchesThread(payload json.RawMessage, threadID string) bool {
	data := parseTrajectoryEventData(payload)
	tid := stringField(data, "session_thread_id")
	if tid == "" {
		tid = primaryThreadID
	}
	return tid == threadID
}
