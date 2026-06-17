package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Team is a coordinated agent group within a session.
type Team struct {
	ID            string
	SessionID     string
	TenantID      string
	Name          string
	Description   string
	LeadThreadID  string
	LeadAgentID   string
	Status        string
	CreatedAt     int64
}

// TeamMember is a named teammate bound to a session thread.
type TeamMember struct {
	ID               string
	TeamID           string
	AgentID          string
	DisplayName      string
	Color            string
	ThreadID         string
	Role             string
	PlanModeRequired bool
	BackendType      string
	Status           string
	JoinedAt         int64
}

// AgentMessage is a mailbox row between team members.
type AgentMessage struct {
	ID           string
	TeamID       string
	FromMemberID string
	ToMemberID   string
	MessageType  string
	Body         string
	Summary      string
	ReadAt       *int64
	CreatedAt    int64
}

// TeamRepo persists teams, members, and mailbox messages.
type TeamRepo struct {
	db *sql.DB
}

// NewTeamRepo returns a team repository.
func NewTeamRepo(db *sql.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

// CreateTeam inserts a team row.
func (r *TeamRepo) CreateTeam(ctx context.Context, team Team) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO teams (
			id, session_id, tenant_id, name, description,
			lead_thread_id, lead_agent_id, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		team.ID,
		team.SessionID,
		team.TenantID,
		team.Name,
		nullString(team.Description),
		team.LeadThreadID,
		team.LeadAgentID,
		team.Status,
		team.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return nil
}

// GetTeamByID returns a team scoped to session.
func (r *TeamRepo) GetTeamByID(
	ctx context.Context,
	sessionID, teamID string,
) (*Team, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, tenant_id, name, description,
		        lead_thread_id, lead_agent_id, status, created_at
		 FROM teams
		 WHERE session_id = ? AND id = ?`,
		sessionID, teamID,
	)
	return scanTeam(row)
}

// GetTeamByName returns a team by session + name.
func (r *TeamRepo) GetTeamByName(
	ctx context.Context,
	sessionID, name string,
) (*Team, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, tenant_id, name, description,
		        lead_thread_id, lead_agent_id, status, created_at
		 FROM teams
		 WHERE session_id = ? AND name = ?`,
		sessionID, name,
	)
	return scanTeam(row)
}

// ListTeamsForSession returns teams for a session.
func (r *TeamRepo) ListTeamsForSession(
	ctx context.Context,
	sessionID string,
) ([]Team, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, tenant_id, name, description,
		        lead_thread_id, lead_agent_id, status, created_at
		 FROM teams
		 WHERE session_id = ?
		 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		team, err := scanTeamRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *team)
	}
	return out, rows.Err()
}

func scanTeam(row *sql.Row) (*Team, error) {
	var team Team
	var desc sql.NullString
	err := row.Scan(
		&team.ID, &team.SessionID, &team.TenantID, &team.Name, &desc,
		&team.LeadThreadID, &team.LeadAgentID, &team.Status, &team.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan team: %w", err)
	}
	if desc.Valid {
		team.Description = desc.String
	}
	return &team, nil
}

func scanTeamRow(rows *sql.Rows) (*Team, error) {
	var team Team
	var desc sql.NullString
	err := rows.Scan(
		&team.ID, &team.SessionID, &team.TenantID, &team.Name, &desc,
		&team.LeadThreadID, &team.LeadAgentID, &team.Status, &team.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if desc.Valid {
		team.Description = desc.String
	}
	return &team, nil
}

// CreateMember inserts a team member row.
func (r *TeamRepo) CreateMember(
	ctx context.Context,
	member TeamMember,
) error {
	plan := 0
	if member.PlanModeRequired {
		plan = 1
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO team_members (
			id, team_id, agent_id, display_name, color, thread_id,
			role, plan_mode_required, backend_type, status, joined_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		member.ID,
		member.TeamID,
		member.AgentID,
		member.DisplayName,
		nullString(member.Color),
		nullString(member.ThreadID),
		nullString(member.Role),
		plan,
		member.BackendType,
		member.Status,
		member.JoinedAt,
	)
	if err != nil {
		return fmt.Errorf("create team member: %w", err)
	}
	return nil
}

// GetMemberByID returns a member by id within a team.
func (r *TeamRepo) GetMemberByID(
	ctx context.Context,
	teamID, memberID string,
) (*TeamMember, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, team_id, agent_id, display_name, color, thread_id,
		        role, plan_mode_required, backend_type, status, joined_at
		 FROM team_members
		 WHERE team_id = ? AND id = ?`,
		teamID, memberID,
	)
	return scanMember(row)
}

// GetMemberByDisplayName returns a member by display name.
func (r *TeamRepo) GetMemberByDisplayName(
	ctx context.Context,
	teamID, displayName string,
) (*TeamMember, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, team_id, agent_id, display_name, color, thread_id,
		        role, plan_mode_required, backend_type, status, joined_at
		 FROM team_members
		 WHERE team_id = ? AND display_name = ?`,
		teamID, displayName,
	)
	return scanMember(row)
}

// ListMembers returns all members for a team.
func (r *TeamRepo) ListMembers(
	ctx context.Context,
	teamID string,
) ([]TeamMember, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, team_id, agent_id, display_name, color, thread_id,
		        role, plan_mode_required, backend_type, status, joined_at
		 FROM team_members
		 WHERE team_id = ?
		 ORDER BY joined_at ASC`,
		teamID,
	)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	defer rows.Close()
	var out []TeamMember
	for rows.Next() {
		member, err := scanMemberRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *member)
	}
	return out, rows.Err()
}

// UpdateMemberStatus sets member status.
func (r *TeamRepo) UpdateMemberStatus(
	ctx context.Context,
	memberID, status string,
) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE team_members SET status = ? WHERE id = ?`,
		status, memberID,
	)
	if err != nil {
		return fmt.Errorf("update member status: %w", err)
	}
	return nil
}

func scanMember(row *sql.Row) (*TeamMember, error) {
	var member TeamMember
	var color, threadID, role sql.NullString
	var plan int
	err := row.Scan(
		&member.ID, &member.TeamID, &member.AgentID, &member.DisplayName,
		&color, &threadID, &role, &plan, &member.BackendType,
		&member.Status, &member.JoinedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan member: %w", err)
	}
	if color.Valid {
		member.Color = color.String
	}
	if threadID.Valid {
		member.ThreadID = threadID.String
	}
	if role.Valid {
		member.Role = role.String
	}
	member.PlanModeRequired = plan != 0
	return &member, nil
}

func scanMemberRow(rows *sql.Rows) (*TeamMember, error) {
	var member TeamMember
	var color, threadID, role sql.NullString
	var plan int
	err := rows.Scan(
		&member.ID, &member.TeamID, &member.AgentID, &member.DisplayName,
		&color, &threadID, &role, &plan, &member.BackendType,
		&member.Status, &member.JoinedAt,
	)
	if err != nil {
		return nil, err
	}
	if color.Valid {
		member.Color = color.String
	}
	if threadID.Valid {
		member.ThreadID = threadID.String
	}
	if role.Valid {
		member.Role = role.String
	}
	member.PlanModeRequired = plan != 0
	return &member, nil
}

// CreateMessage inserts an agent_messages row.
func (r *TeamRepo) CreateMessage(
	ctx context.Context,
	msg AgentMessage,
) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO agent_messages (
			id, team_id, from_member_id, to_member_id, message_type,
			body, summary, read_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID,
		msg.TeamID,
		msg.FromMemberID,
		nullString(msg.ToMemberID),
		msg.MessageType,
		msg.Body,
		nullString(msg.Summary),
		nullInt64(msg.ReadAt),
		msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create agent message: %w", err)
	}
	return nil
}

// ListUnreadMessages returns unread messages for a recipient member.
func (r *TeamRepo) ListUnreadMessages(
	ctx context.Context,
	teamID, recipientMemberID string,
	limit int,
) ([]AgentMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, team_id, from_member_id, to_member_id, message_type,
		        body, summary, read_at, created_at
		 FROM agent_messages
		 WHERE team_id = ?
		   AND read_at IS NULL
		   AND (to_member_id = ? OR to_member_id IS NULL OR to_member_id = '')
		 ORDER BY created_at ASC
		 LIMIT ?`,
		teamID, recipientMemberID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list unread messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ListMessages returns recent messages for a team.
func (r *TeamRepo) ListMessages(
	ctx context.Context,
	teamID string,
	limit int,
) ([]AgentMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, team_id, from_member_id, to_member_id, message_type,
		        body, summary, read_at, created_at
		 FROM agent_messages
		 WHERE team_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		teamID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	// Return chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// MarkMessagesRead marks messages read for a recipient.
func (r *TeamRepo) MarkMessagesRead(
	ctx context.Context,
	teamID, recipientMemberID string,
	messageIDs []string,
) (int64, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	placeholders := make([]string, len(messageIDs))
	args := []any{now, teamID, recipientMemberID}
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(
		`UPDATE agent_messages SET read_at = ?
		 WHERE team_id = ?
		   AND (to_member_id = ? OR to_member_id IS NULL OR to_member_id = '')
		   AND id IN (%s)`,
		joinPlaceholders(placeholders),
	)
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark messages read: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func scanMessages(rows *sql.Rows) ([]AgentMessage, error) {
	var out []AgentMessage
	for rows.Next() {
		var msg AgentMessage
		var toMember, summary sql.NullString
		var readAt sql.NullInt64
		if err := rows.Scan(
			&msg.ID, &msg.TeamID, &msg.FromMemberID, &toMember,
			&msg.MessageType, &msg.Body, &summary, &readAt, &msg.CreatedAt,
		); err != nil {
			return nil, err
		}
		if toMember.Valid {
			msg.ToMemberID = toMember.String
		}
		if summary.Valid {
			msg.Summary = summary.String
		}
		if readAt.Valid {
			v := readAt.Int64
			msg.ReadAt = &v
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func joinPlaceholders(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
