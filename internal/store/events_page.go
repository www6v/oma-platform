package store

import "context"

// EventsPage is a paginated slice of session events.
type EventsPage struct {
	Items    []StoredEvent
	HasMore  bool
	LastSeq  int
}

// ListEventsPage returns up to limit events after afterSeq (ascending).
func (r *EventRepo) ListEventsPage(
	ctx context.Context,
	sessionID string,
	afterSeq, limit int,
) (*EventsPage, error) {
	if limit <= 0 {
		limit = 100
	}
	fetch := limit + 1
	list, err := r.ListEvents(ctx, sessionID, afterSeq, fetch, true)
	if err != nil {
		return nil, err
	}
	page := &EventsPage{Items: list}
	if len(list) > limit {
		page.HasMore = true
		page.Items = list[:limit]
	}
	if n := len(page.Items); n > 0 {
		page.LastSeq = page.Items[n-1].Seq
	}
	return page, nil
}
