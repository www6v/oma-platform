package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
)

type scheduleWakeupRequest struct {
	DelaySeconds *int    `json:"delay_seconds"`
	At           *string `json:"at"`
	Cron         *string `json:"cron"`
	Prompt       string  `json:"prompt"`
}

type scheduleWakeupResponse struct {
	ID     string  `json:"id"`
	FireAt *string `json:"fire_at,omitempty"`
	Cron   *string `json:"cron,omitempty"`
	Kind   string  `json:"kind"`
}

type wakeupListItem struct {
	ID     string  `json:"id"`
	FireAt *string `json:"fire_at,omitempty"`
	Cron   *string `json:"cron,omitempty"`
	Prompt string  `json:"prompt"`
	Kind   string  `json:"kind"`
}

func mountWakeupInternalRoutes(r chi.Router, h *sessionHandlers) {
	r.Route("/sessions/{id}/wakeups", func(r chi.Router) {
		r.Get("/", handleInternalListWakeups(h))
		r.Post("/", handleInternalScheduleWakeup(h))
		r.Delete("/{wakeupId}", handleInternalCancelWakeup(h))
	})
}

func handleInternalScheduleWakeup(h *sessionHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sessionID := chi.URLParam(req, "id")
		var body scheduleWakeupRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		resp, err := h.ScheduleWakeup(req.Context(), sessionID, body)
		if err != nil {
			writeScheduleWakeupError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

func handleInternalCancelWakeup(h *sessionHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sessionID := chi.URLParam(req, "id")
		wakeupID := chi.URLParam(req, "wakeupId")
		ok, err := h.CancelWakeup(req.Context(), sessionID, wakeupID)
		if err != nil {
			writeScheduleWakeupError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"cancelled": ok})
	}
}

func handleInternalListWakeups(h *sessionHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sessionID := chi.URLParam(req, "id")
		items, err := h.ListWakeups(req.Context(), sessionID)
		if err != nil {
			writeScheduleWakeupError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedules": items})
	}
}

func writeScheduleWakeupError(w http.ResponseWriter, err error) {
	switch err {
	case store.ErrNotFound:
		writeError(w, http.StatusNotFound, "not found")
	case errSessionArchived:
		writeError(w, http.StatusConflict, "session archived")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

var errSessionArchived = fmt.Errorf("session is terminated; cannot schedule wakeup")

// ScheduleWakeup registers a durable self-wakeup for a session.
func (h *sessionHandlers) ScheduleWakeup(
	ctx context.Context,
	sessionID string,
	args scheduleWakeupRequest,
) (scheduleWakeupResponse, error) {
	if h.wakeups == nil {
		return scheduleWakeupResponse{}, fmt.Errorf("wakeup schedules unavailable")
	}
	sess, err := h.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return scheduleWakeupResponse{}, err
	}
	if sess == nil {
		return scheduleWakeupResponse{}, store.ErrNotFound
	}
	if sess.Status == store.SessionStatusArchived {
		return scheduleWakeupResponse{}, errSessionArchived
	}

	prompt := stringsTrim(args.Prompt)
	if prompt == "" {
		return scheduleWakeupResponse{}, fmt.Errorf("prompt is required")
	}

	provided := 0
	if args.DelaySeconds != nil {
		provided++
	}
	if args.At != nil && *args.At != "" {
		provided++
	}
	if args.Cron != nil && *args.Cron != "" {
		provided++
	}
	if provided != 1 {
		return scheduleWakeupResponse{}, fmt.Errorf(
			"must provide exactly one of delay_seconds | at | cron",
		)
	}

	pending, err := h.wakeups.CountPending(ctx, sessionID)
	if err != nil {
		return scheduleWakeupResponse{}, err
	}
	if pending >= store.MaxPendingWakeups {
		return scheduleWakeupResponse{}, fmt.Errorf(
			"pending wakeup cap reached (%d/%d); "+
				"call list_schedules to inspect, cancel_schedule to free a slot",
			pending, store.MaxPendingWakeups,
		)
	}

	now := time.Now()
	var kind store.WakeupKind
	var fireAt time.Time
	var cronExpr string

	if args.DelaySeconds != nil {
		sec := *args.DelaySeconds
		if sec < 5 || sec > 7*24*3600 {
			return scheduleWakeupResponse{}, fmt.Errorf(
				"delay_seconds must be between 5 and 604800",
			)
		}
		kind = store.WakeupKindOneShot
		fireAt = now.Add(time.Duration(sec) * time.Second)
	} else if args.At != nil {
		parsed, err := time.Parse(time.RFC3339, *args.At)
		if err != nil {
			return scheduleWakeupResponse{}, fmt.Errorf(
				"invalid 'at' timestamp: %s", *args.At,
			)
		}
		kind = store.WakeupKindOneShot
		fireAt = parsed
	} else {
		cronExpr = *args.Cron
		if len(cronExpr) < 9 || len(cronExpr) > 120 {
			return scheduleWakeupResponse{}, fmt.Errorf("invalid cron expression")
		}
		parser := cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		)
		schedule, err := parser.Parse(cronExpr)
		if err != nil {
			return scheduleWakeupResponse{}, fmt.Errorf("invalid cron: %w", err)
		}
		kind = store.WakeupKindCron
		fireAt = schedule.Next(now)
	}

	scheduledAt := now.UTC().Format(time.RFC3339Nano)
	spanEventID := store.NewEventID()
	scheduleID := store.NewScheduleID()

	row := store.WakeupSchedule{
		ID:            scheduleID,
		TenantID:      sess.TenantID,
		SessionID:     sessionID,
		Prompt:        prompt,
		Kind:          kind,
		Cron:          cronExpr,
		FireAt:        fireAt.Unix(),
		ParentEventID: spanEventID,
		SpanEventID:   spanEventID,
		ScheduledAt:   scheduledAt,
		CreatedAt:     now.UnixMilli(),
	}
	if err := h.wakeups.Create(ctx, row); err != nil {
		return scheduleWakeupResponse{}, err
	}

	fireAtISO := fireAt.UTC().Format(time.RFC3339)
	spanPayload, err := json.Marshal(map[string]any{
		"type":        "span.wakeup_scheduled",
		"id":          spanEventID,
		"schedule_id": scheduleID,
		"fire_at":     fireAtISO,
		"kind":        string(kind),
		"cron":        optionalCron(kind, cronExpr),
	})
	if err != nil {
		return scheduleWakeupResponse{}, err
	}
	if err := h.appendAndPublish(ctx, sessionID, spanPayload); err != nil {
		return scheduleWakeupResponse{}, err
	}

	resp := scheduleWakeupResponse{
		ID:     scheduleID,
		FireAt: &fireAtISO,
		Kind:   string(kind),
	}
	if kind == store.WakeupKindCron {
		resp.Cron = &cronExpr
	}
	return resp, nil
}

// CancelWakeup removes a pending schedule by id.
func (h *sessionHandlers) CancelWakeup(
	ctx context.Context,
	sessionID, wakeupID string,
) (bool, error) {
	if h.wakeups == nil {
		return false, fmt.Errorf("wakeup schedules unavailable")
	}
	if wakeupID == "" {
		return false, nil
	}
	return h.wakeups.Delete(ctx, sessionID, wakeupID)
}

// ListWakeups returns pending schedules for a session.
func (h *sessionHandlers) ListWakeups(
	ctx context.Context,
	sessionID string,
) ([]wakeupListItem, error) {
	if h.wakeups == nil {
		return nil, fmt.Errorf("wakeup schedules unavailable")
	}
	rows, err := h.wakeups.ListForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]wakeupListItem, 0, len(rows))
	for _, row := range rows {
		fireAt := row.FireAtISO()
		item := wakeupListItem{
			ID:     row.ID,
			FireAt: &fireAt,
			Prompt: row.Prompt,
			Kind:   string(row.Kind),
		}
		if row.Kind == store.WakeupKindCron && row.Cron != "" {
			item.Cron = &row.Cron
		}
		out = append(out, item)
	}
	return out, nil
}

// FireScheduledWakeup injects a synthetic user.message and runs a turn.
func (h *sessionHandlers) FireScheduledWakeup(
	ctx context.Context,
	row store.WakeupSchedule,
) error {
	sess, err := h.sessions.Get(ctx, row.TenantID, row.SessionID)
	if err != nil {
		return err
	}
	if sess == nil || sess.Status == store.SessionStatusArchived {
		return nil
	}
	h.registerMachine(sess)

	userMsg := map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": row.Prompt},
		},
		"metadata": map[string]any{
			"harness":      "schedule",
			"kind":         "wakeup",
			"wakeup_kind":  string(row.Kind),
			"scheduled_at": row.ScheduledAt,
			"fired_at":     time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if row.ParentEventID != "" {
		userMsg["parent_event_id"] = row.ParentEventID
	}
	payload, err := json.Marshal(userMsg)
	if err != nil {
		return err
	}
	return h.registry.EnqueueEvents(
		ctx, row.SessionID, []json.RawMessage{payload}, true, false, nil,
	)
}

func (h *sessionHandlers) appendAndPublish(
	ctx context.Context,
	sessionID string,
	payload json.RawMessage,
) error {
	return h.appendAndPublishBatch(ctx, sessionID, []json.RawMessage{payload})
}

func (h *sessionHandlers) appendAndPublishBatch(
	ctx context.Context,
	sessionID string,
	payloads []json.RawMessage,
) error {
	if len(payloads) == 0 {
		return nil
	}
	stored, err := h.events.AppendEvents(ctx, sessionID, payloads)
	if err != nil {
		return err
	}
	for _, ev := range stored {
		h.hub.Publish(sessionID, stream.Event{
			Seq:     ev.Seq,
			Payload: ev.Payload,
		})
	}
	return nil
}

func optionalCron(kind store.WakeupKind, cronExpr string) *string {
	if kind == store.WakeupKindCron && cronExpr != "" {
		return &cronExpr
	}
	return nil
}

func stringsTrim(s string) string {
	return strings.TrimSpace(s)
}
