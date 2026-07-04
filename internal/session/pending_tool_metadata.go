package session

import (
	"context"
	"encoding/json"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func (m *Machine) syncPendingToolCallsMetadata(
	ctx context.Context,
	eventPayloads []json.RawMessage,
	stopReason map[string]any,
) error {
	if m.Sessions == nil {
		return nil
	}
	var pending []harness.PendingToolCall
	if stopReason["type"] == "requires_action" {
		pending = harness.BuildPendingToolCalls(
			eventPayloads,
			harness.PendingCustomToolIDs(eventPayloads),
		)
	}
	patch, err := json.Marshal(map[string]any{
		"pending_tool_calls": pending,
	})
	if err != nil {
		return err
	}
	_, err = m.Sessions.Update(ctx, m.TenantID, m.SessionID, store.UpdateSessionInput{
		Metadata:    patch,
		MetadataSet: true,
	})
	return err
}
