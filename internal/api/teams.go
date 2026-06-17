package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/open-ma/oma-building/internal/store"
)

func writeTeamError(w http.ResponseWriter, err error) {
	switch err {
	case store.ErrNotFound:
		writeError(w, http.StatusNotFound, "not found")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// ListTeams returns teams with members for a session (read-only; writes in harness).
func (h *sessionHandlers) ListTeams(
	ctx context.Context,
	sessionID string,
) ([]map[string]any, error) {
	if h.teams == nil {
		return nil, fmt.Errorf("team tools unavailable")
	}
	sess, err := h.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, store.ErrNotFound
	}
	rows, err := h.teams.ListTeamsForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, team := range rows {
		members, err := h.teams.ListMembers(ctx, team.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, serializeTeam(team, members))
	}
	return out, nil
}

func serializeTeam(team store.Team, members []store.TeamMember) map[string]any {
	memberItems := make([]map[string]any, 0, len(members))
	for _, m := range members {
		memberItems = append(memberItems, serializeMember(m))
	}
	return map[string]any{
		"id":             team.ID,
		"session_id":     team.SessionID,
		"name":           team.Name,
		"description":    nullIfEmpty(team.Description),
		"lead_thread_id": team.LeadThreadID,
		"lead_agent_id":  team.LeadAgentID,
		"status":         team.Status,
		"created_at":     formatISO(team.CreatedAt),
		"members":        memberItems,
	}
}

func serializeMember(m store.TeamMember) map[string]any {
	return map[string]any{
		"id":           m.ID,
		"team_id":      m.TeamID,
		"agent_id":     m.AgentID,
		"display_name": m.DisplayName,
		"color":        nullIfEmpty(m.Color),
		"thread_id":    nullIfEmpty(m.ThreadID),
		"role":         nullIfEmpty(m.Role),
		"backend_type": m.BackendType,
		"status":       m.Status,
		"joined_at":    formatISO(m.JoinedAt),
	}
}

func serializeMessage(m store.AgentMessage) map[string]any {
	out := map[string]any{
		"id":             m.ID,
		"team_id":        m.TeamID,
		"from_member_id": m.FromMemberID,
		"message_type":   m.MessageType,
		"body":           m.Body,
		"summary":        nullIfEmpty(m.Summary),
		"created_at":     formatISO(m.CreatedAt),
	}
	if m.ToMemberID != "" {
		out["to_member_id"] = m.ToMemberID
	}
	if m.ReadAt != nil {
		out["read_at"] = formatISO(*m.ReadAt)
	}
	return out
}
