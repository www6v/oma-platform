package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func (h *sessionHandlers) applySessionResources(
	ctx context.Context,
	tenantID string,
	sess *store.Session,
	raw json.RawMessage,
) (*store.Session, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return sess, nil
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("invalid resources")
	}
	specs := harness.CoerceResourceMaps(items)
	if len(specs) == 0 {
		return sess, nil
	}
	if h.resources == nil {
		return nil, fmt.Errorf("resources not configured")
	}
	scoped := h.resources.ScopeSessionResources(
		ctx, tenantID, sess.ID, specs,
	)
	data, err := json.Marshal(scoped)
	if err != nil {
		return nil, fmt.Errorf("marshal session resources: %w", err)
	}
	return h.sessions.SetResources(ctx, tenantID, sess.ID, data)
}
