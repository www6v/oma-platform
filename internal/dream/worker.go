package dream

import (
	"context"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

// Worker runs the MVP dream pipeline: copy input memories to output store.
type Worker struct {
	Dreams       *store.DreamRepo
	MemoryStores *store.MemoryStoreRepo
	Sessions     *store.SessionRepo
}

// TickResult summarizes one worker pass.
type TickResult struct {
	Processed int
	Total     int
}

// Tick processes all active dreams once.
func (w *Worker) Tick(ctx context.Context) (TickResult, error) {
	if w == nil || w.Dreams == nil || w.MemoryStores == nil {
		return TickResult{}, nil
	}
	active, err := w.Dreams.ListActive(ctx)
	if err != nil {
		return TickResult{}, err
	}
	result := TickResult{Total: len(active)}
	for i := range active {
		row := active[i]
		if err := w.Process(ctx, row.TenantID, row.ID); err != nil {
			_ = w.Dreams.MarkFailed(ctx, row.TenantID, row.ID, store.DreamError{
				Type:    "internal_error",
				Message: err.Error(),
			})
		}
		result.Processed++
	}
	return result, nil
}

// Process executes one dream through pending → completed.
func (w *Worker) Process(
	ctx context.Context,
	tenantID, dreamID string,
) error {
	dream, err := w.Dreams.Get(ctx, tenantID, dreamID)
	if err != nil {
		return err
	}
	if dream == nil {
		return store.ErrDreamNotFound
	}
	if storeDreamTerminal(dream.Status) {
		return nil
	}
	if dream.Status == store.DreamStatusCanceled {
		return nil
	}

	inputStore, err := w.MemoryStores.GetStore(
		ctx, tenantID, dream.InputMemoryStoreID,
	)
	if err != nil {
		return err
	}
	if inputStore == nil || inputStore.ArchivedAt.Valid {
		return w.Dreams.MarkFailed(ctx, tenantID, dreamID, store.DreamError{
			Type:    "input_memory_store_unavailable",
			Message: "input memory store unavailable",
		})
	}
	if w.Sessions != nil {
		for _, sid := range dream.InputSessionIDs {
			sess, err := w.Sessions.Get(ctx, tenantID, sid)
			if err != nil {
				return err
			}
			if sess == nil || sess.ArchivedAt != nil {
				return w.Dreams.MarkFailed(ctx, tenantID, dreamID, store.DreamError{
					Type:    "input_session_unavailable",
					Message: fmt.Sprintf("input session %s unavailable", sid),
				})
			}
		}
	}

	var outputStoreID string
	if dream.OutputMemoryStoreID.Valid {
		outputStoreID = dream.OutputMemoryStoreID.String
	} else {
		out, err := w.MemoryStores.CreateStore(
			ctx, tenantID,
			fmt.Sprintf("dream-%s", dream.ID),
			ptrString(fmt.Sprintf(
				"Curated by %s from input %s",
				dream.ID, dream.InputMemoryStoreID,
			)),
		)
		if err != nil {
			return err
		}
		outputStoreID = out.ID
		if err := w.Dreams.MarkRunning(
			ctx, tenantID, dreamID, outputStoreID, nil,
		); err != nil && err != store.ErrDreamInvalidState {
			return err
		}
	}

	dream, err = w.Dreams.Get(ctx, tenantID, dreamID)
	if err != nil {
		return err
	}
	if dream == nil || dream.Status == store.DreamStatusCanceled {
		return nil
	}
	if dream.Status != store.DreamStatusRunning {
		return nil
	}

	memories, err := w.MemoryStores.ListMemories(
		ctx, tenantID, dream.InputMemoryStoreID, "",
	)
	if err != nil {
		return err
	}
	for _, mem := range memories {
		if _, err := w.MemoryStores.WriteMemory(
			ctx, tenantID, outputStoreID, mem.Path, mem.Content,
			"dream", dreamID, nil,
		); err != nil {
			return err
		}
	}

	return w.Dreams.MarkCompleted(ctx, tenantID, dreamID)
}

func storeDreamTerminal(status store.DreamStatus) bool {
	switch status {
	case store.DreamStatusCompleted,
		store.DreamStatusFailed,
		store.DreamStatusCanceled:
		return true
	default:
		return false
	}
}

func ptrString(v string) *string {
	return &v
}
