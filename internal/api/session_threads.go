package api

import (
	"encoding/json"
	"sort"

	"github.com/open-ma/oma-building/internal/store"
)

const primaryThreadID = "sthr_primary"

type derivedThread struct {
	id             string
	agentID        string
	agentName      string
	parentThreadID *string
	createdAt      int64
	status         string
	archivedAt     *int64
}

func deriveSessionThreads(
	sess *store.Session,
	events []store.StoredEvent,
	includeArchived bool,
) []map[string]any {
	threads := map[string]*derivedThread{
		primaryThreadID: {
			id:        primaryThreadID,
			agentID:   sess.AgentID,
			agentName: agentNameFromSnapshot(sess.AgentSnapshot),
			createdAt: sess.CreatedAt,
			status:    "active",
		},
	}

	for _, ev := range events {
		data := parseTrajectoryEventData(ev.Payload)
		switch ev.Type {
		case "session.thread_created":
			tid := stringField(data, "session_thread_id")
			if tid == "" || tid == primaryThreadID {
				continue
			}
			if _, exists := threads[tid]; exists {
				continue
			}
			parent := stringField(data, "parent_thread_id")
			var parentPtr *string
			if parent != "" {
				parentPtr = &parent
			}
			threads[tid] = &derivedThread{
				id:             tid,
				agentID:        stringField(data, "agent_id"),
				agentName:      stringField(data, "agent_name"),
				parentThreadID: parentPtr,
				createdAt:      ev.CreatedAt,
				status:         "active",
			}
		}
	}

	ordered := make([]*derivedThread, 0, len(threads))
	ordered = append(ordered, threads[primaryThreadID])
	subs := make([]*derivedThread, 0, len(threads)-1)
	for id, th := range threads {
		if id == primaryThreadID {
			continue
		}
		subs = append(subs, th)
	}
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].createdAt < subs[j].createdAt
	})
	ordered = append(ordered, subs...)

	out := make([]map[string]any, 0, len(ordered))
	for _, th := range ordered {
		if th.status == "archived" && !includeArchived {
			continue
		}
		out = append(out, serializeSessionThread(sess.ID, th))
	}
	return out
}

func serializeSessionThread(
	sessionID string,
	th *derivedThread,
) map[string]any {
	updatedAt := th.createdAt
	if th.archivedAt != nil {
		updatedAt = *th.archivedAt
	}
	var parent any
	if th.parentThreadID != nil {
		parent = *th.parentThreadID
	}
	var archivedAt any
	if th.archivedAt != nil {
		archivedAt = formatISO(*th.archivedAt)
	}
	return map[string]any{
		"id":               th.id,
		"type":             "session_thread",
		"session_id":       sessionID,
		"session_thread_id": th.id,
		"agent_id":         nullIfEmpty(th.agentID),
		"agent_name":       nullIfEmpty(th.agentName),
		"parent_thread_id": parent,
		"status":           th.status,
		"created_at":       formatISO(th.createdAt),
		"archived_at":      archivedAt,
		"updated_at":       formatISO(updatedAt),
	}
}

func agentNameFromSnapshot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	return cfg.Name
}

func stringField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key].(string)
	if !ok {
		return ""
	}
	return v
}
