package api

import (
	"encoding/json"
	"net/http"

	"github.com/open-ma/oma-building/internal/store"
)

type pendingListItem struct {
	PendingSeq      int             `json:"pending_seq"`
	EnqueuedAt      int64           `json:"enqueued_at"`
	SessionThreadID string          `json:"session_thread_id"`
	Type            string          `json:"type"`
	EventID         string          `json:"event_id"`
	CancelledAt     *int64          `json:"cancelled_at"`
	Data            json.RawMessage `json:"data"`
}

func formatPendingRow(row store.PendingRow) pendingListItem {
	return pendingListItem{
		PendingSeq:      row.PendingSeq,
		EnqueuedAt:      row.EnqueuedAt,
		SessionThreadID: row.SessionThreadID,
		Type:            row.Type,
		EventID:         row.EventID,
		CancelledAt:     row.CancelledAt,
		Data:            row.Data,
	}
}

func (h *sessionHandlers) listSessionPending(
	req *http.Request,
	sessionID string,
) ([]pendingListItem, error) {
	threadID := req.URL.Query().Get("session_thread_id")
	if threadID == "" {
		threadID = "sthr_primary"
	}
	includeCancelled := req.URL.Query().Get("include_cancelled") == "true"
	rows, err := h.pending.List(
		req.Context(), sessionID, threadID, includeCancelled,
	)
	if err != nil {
		return nil, err
	}
	out := make([]pendingListItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, formatPendingRow(row))
	}
	return out, nil
}
