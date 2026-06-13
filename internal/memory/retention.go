package memory

import (
	"context"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

const (
	defaultRetentionDays  = 30
	defaultSweepHourUTC   = 3
	defaultSweepMinuteUTC = 0
)

// RetentionWorker prunes old memory_versions rows on a daily schedule.
type RetentionWorker struct {
	MemoryStores   *store.MemoryStoreRepo
	Now            func() time.Time
	SweepHourUTC   int
	SweepMinuteUTC int
	RetentionDays  int
}

// TickResult summarizes one retention pass.
type TickResult struct {
	Ran     bool
	Removed int64
}

// Tick runs the retention sweep when the UTC clock matches the configured gate.
func (w *RetentionWorker) Tick(ctx context.Context) (TickResult, error) {
	if w == nil || w.MemoryStores == nil {
		return TickResult{}, nil
	}
	now := w.now()
	if now.UTC().Hour() != w.sweepHour() ||
		now.UTC().Minute() != w.sweepMinute() {
		return TickResult{}, nil
	}
	removed, err := w.prune(ctx, now)
	return TickResult{Ran: true, Removed: removed}, err
}

// RunOnce prunes without the daily gate (for tests and manual runs).
func (w *RetentionWorker) RunOnce(ctx context.Context) (int64, error) {
	if w == nil || w.MemoryStores == nil {
		return 0, nil
	}
	return w.prune(ctx, w.now())
}

func (w *RetentionWorker) prune(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	cutoff := now.Add(-time.Duration(w.retentionDays()) * 24 * time.Hour).
		UnixMilli()
	return w.MemoryStores.PruneVersionsOlderThan(ctx, cutoff)
}

func (w *RetentionWorker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *RetentionWorker) sweepHour() int {
	if w.SweepHourUTC != 0 || w.SweepMinuteUTC != 0 {
		return w.SweepHourUTC
	}
	return defaultSweepHourUTC
}

func (w *RetentionWorker) sweepMinute() int {
	if w.SweepHourUTC != 0 || w.SweepMinuteUTC != 0 {
		return w.SweepMinuteUTC
	}
	return defaultSweepMinuteUTC
}

func (w *RetentionWorker) retentionDays() int {
	if w.RetentionDays > 0 {
		return w.RetentionDays
	}
	return defaultRetentionDays
}
