package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

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

func (h *sessionHandlers) handleSessionTeamMessages(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	teamID := chi.URLParam(req, "team_id")
	limit := 100
	if raw := req.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := h.ListTeamMessages(req.Context(), sess.ID, teamID, limit)
	if err != nil {
		writeTeamError(w, err)
		return
	}
	if msgs == nil {
		msgs = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": msgs})
}

func (h *sessionHandlers) handleSessionTeamMemberShutdown(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if sess.Status == store.SessionStatusArchived {
		writeError(w, http.StatusConflict, "session archived")
		return
	}
	teamID := chi.URLParam(req, "team_id")
	memberID := chi.URLParam(req, "member_id")
	if err := h.ShutdownTeamMember(
		req.Context(), sess.ID, teamID, memberID,
	); err != nil {
		writeTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ListTeamMessages returns mailbox rows for a team (Console read path).
func (h *sessionHandlers) ListTeamMessages(
	ctx context.Context,
	sessionID, teamID string,
	limit int,
) ([]map[string]any, error) {
	if h.teams == nil {
		return nil, fmt.Errorf("team tools unavailable")
	}
	team, err := h.teams.GetTeamByID(ctx, sessionID, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, store.ErrNotFound
	}
	rows, err := h.teams.ListMessages(ctx, teamID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, serializeMessage(m))
	}
	return out, nil
}

// ShutdownTeamMember queues a shutdown_request for a teammate (Console).
func (h *sessionHandlers) ShutdownTeamMember(
	ctx context.Context,
	sessionID, teamID, memberID string,
) error {
	if h.teams == nil {
		return fmt.Errorf("team tools unavailable")
	}
	team, err := h.teams.GetTeamByID(ctx, sessionID, teamID)
	if err != nil {
		return err
	}
	if team == nil {
		return store.ErrNotFound
	}
	target, err := h.teams.GetMemberByID(ctx, teamID, memberID)
	if err != nil {
		return err
	}
	if target == nil {
		return store.ErrNotFound
	}
	if target.Status == "shutdown" {
		return fmt.Errorf("member already shutdown")
	}
	if target.Status == "shutting_down" {
		return fmt.Errorf("member shutdown already in progress")
	}
	if target.BackendType != "in_process" {
		return fmt.Errorf("shutdown only supported for in_process members")
	}

	members, err := h.teams.ListMembers(ctx, teamID)
	if err != nil {
		return err
	}
	fromMember := pickShutdownSender(members, team, memberID)
	if fromMember == nil {
		return fmt.Errorf("no sender member available for shutdown")
	}

	if err := h.teams.UpdateMemberStatus(
		ctx, memberID, "shutting_down",
	); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	msgID := store.NewTeamMessageID()
	body := "Shutdown requested from Console"
	msg := store.AgentMessage{
		ID:           msgID,
		TeamID:       teamID,
		FromMemberID: fromMember.ID,
		ToMemberID:   memberID,
		MessageType:  "shutdown_request",
		Body:         body,
		CreatedAt:    now,
	}
	if err := h.teams.CreateMessage(ctx, msg); err != nil {
		return err
	}

	shuttingDown := map[string]any{
		"type":              "team.member_shutting_down",
		"team_id":           teamID,
		"member_id":         memberID,
		"display_name":      target.DisplayName,
		"session_thread_id": target.ThreadID,
	}
	teamMsg := map[string]any{
		"type":              "team.message",
		"team_id":           teamID,
		"message_id":        msgID,
		"from_member_id":    fromMember.ID,
		"from_display_name": fromMember.DisplayName,
		"to":                target.DisplayName,
		"to_member_id":      memberID,
		"message_type":      "shutdown_request",
		"body":              body,
	}
	if target.ThreadID != "" {
		teamMsg["session_thread_id"] = target.ThreadID
	}
	payloads := make([]json.RawMessage, 0, 2)
	for _, ev := range []map[string]any{shuttingDown, teamMsg} {
		raw, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		payloads = append(payloads, raw)
	}
	return h.appendAndPublishBatch(ctx, sessionID, payloads)
}

func pickShutdownSender(
	members []store.TeamMember,
	team *store.Team,
	targetMemberID string,
) *store.TeamMember {
	for i := range members {
		m := &members[i]
		if m.ID == targetMemberID {
			continue
		}
		if m.AgentID == team.LeadAgentID ||
			m.ThreadID == team.LeadThreadID {
			return m
		}
	}
	for i := range members {
		m := &members[i]
		if m.ID != targetMemberID {
			return m
		}
	}
	return nil
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

func (h *sessionHandlers) handleSessionTeamTasks(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	teamID := chi.URLParam(req, "team_id")
	tasks, err := h.listTeamTasks(req.Context(), sess.ID, teamID)
	if err != nil {
		writeTeamError(w, err)
		return
	}
	if tasks == nil {
		tasks = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tasks})
}

func (h *sessionHandlers) listTeamTasks(
	ctx context.Context,
	sessionID, teamID string,
) ([]map[string]any, error) {
	if h.tasks == nil {
		return nil, fmt.Errorf("task board unavailable")
	}
	team, err := h.teams.GetTeamByID(ctx, sessionID, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, store.ErrNotFound
	}
	rows, err := h.tasks.ListTasks(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, serializeTask(t))
	}
	return out, nil
}

func serializeTask(t store.TeamTask) map[string]any {
	blocks := t.Blocks
	if blocks == nil {
		blocks = []string{}
	}
	blockedBy := t.BlockedBy
	if blockedBy == nil {
		blockedBy = []string{}
	}
	return map[string]any{
		"id":              t.ID,
		"team_id":         t.TeamID,
		"subject":         t.Subject,
		"description":     nullIfEmpty(t.Description),
		"active_form":     nullIfEmpty(t.ActiveForm),
		"owner_member_id": nullIfEmpty(t.OwnerMemberID),
		"status":          t.Status,
		"blocks":          blocks,
		"blocked_by":      blockedBy,
		"created_at":      formatISO(t.CreatedAt),
		"updated_at":      formatISO(t.UpdatedAt),
	}
}
