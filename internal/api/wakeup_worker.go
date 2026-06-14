package api

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/open-ma/oma-building/internal/store"
)

// WakeupWorker fires due session wakeup schedules.
type WakeupWorker struct {
	Wakeups  *store.WakeupRepo
	Sessions *sessionHandlers
}

// WakeupTickResult summarizes one worker pass.
type WakeupTickResult struct {
	Fired int
	Total int
}

// TickWakeupSchedules processes due wakeup schedules once.
func (w *WakeupWorker) Tick(ctx context.Context) (WakeupTickResult, error) {
	if w == nil || w.Wakeups == nil || w.Sessions == nil {
		return WakeupTickResult{}, nil
	}
	now := time.Now().Unix()
	due, err := w.Wakeups.ListDue(ctx, now, 50)
	if err != nil {
		return WakeupTickResult{}, err
	}
	result := WakeupTickResult{Total: len(due)}
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)
	for i := range due {
		row := due[i]
		if err := w.Sessions.FireScheduledWakeup(ctx, row); err != nil {
			return result, fmt.Errorf("fire wakeup %s: %w", row.ID, err)
		}
		if row.Kind == store.WakeupKindOneShot {
			if _, err := w.Wakeups.Delete(ctx, row.SessionID, row.ID); err != nil {
				return result, err
			}
		} else if row.Cron != "" {
			schedule, err := parser.Parse(row.Cron)
			if err != nil {
				if _, delErr := w.Wakeups.Delete(
					ctx, row.SessionID, row.ID,
				); delErr != nil {
					return result, delErr
				}
				continue
			}
			next := schedule.Next(time.Now())
			if err := w.Wakeups.UpdateFireAt(ctx, row.ID, next.Unix()); err != nil {
				return result, err
			}
		}
		result.Fired++
	}
	return result, nil
}
