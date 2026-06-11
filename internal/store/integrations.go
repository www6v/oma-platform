package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IntegrationProvider is linear, github, or slack.
type IntegrationProvider string

const (
	ProviderLinear IntegrationProvider = "linear"
	ProviderGitHub IntegrationProvider = "github"
	ProviderSlack  IntegrationProvider = "slack"
)

// IntegrationInstallation is a provider workspace install row.
type IntegrationInstallation struct {
	ID            string
	TenantID      string
	UserID        string
	ProviderID    string
	WorkspaceID   string
	WorkspaceName string
	InstallKind   string
	AppID         *string
	BotUserID     string
	VaultID       *string
	CreatedAt     int64
	RevokedAt     *int64
}

// IntegrationPublication is an agent↔workspace binding row.
type IntegrationPublication struct {
	ID                 string
	TenantID           string
	UserID             string
	AgentID            string
	InstallationID     string
	EnvironmentID      *string
	Mode               string
	Status             string
	PersonaName        string
	PersonaAvatarURL   *string
	Capabilities       []string
	SessionGranularity string
	CreatedAt          int64
	UnpublishedAt      *int64
	ClientID           *string
	ClientSecretCipher *string
	WebhookSecretCipher *string
	SigningSecretCipher *string
	VaultID            *string
	ReturnURL          *string
}

// LinearDispatchRule is a Linear auto-pickup rule.
type LinearDispatchRule struct {
	ID                  string
	TenantID            string
	PublicationID       string
	Name                string
	Enabled             bool
	FilterLabel         *string
	FilterStates        []string
	FilterProjectID     *string
	MaxConcurrent       int
	PollIntervalSeconds int
	LastPolledAt        *int64
	CreatedAt           int64
	UpdatedAt           int64
}

// NewLinearDispatchRule input for insert.
type NewLinearDispatchRule struct {
	TenantID            string
	PublicationID       string
	Name                string
	Enabled             bool
	FilterLabel         *string
	FilterStates        []string
	FilterProjectID     *string
	MaxConcurrent       int
	PollIntervalSeconds int
}

// LinearDispatchRulePatch partial update.
type LinearDispatchRulePatch struct {
	Name                *string
	Enabled             *bool
	FilterLabel         **string
	FilterStates        *[]string
	FilterProjectID     **string
	MaxConcurrent       *int
	PollIntervalSeconds *int
}

// NewPublicationShell input for publication-first create.
type NewPublicationShell struct {
	TenantID           string
	UserID             string
	AgentID            string
	EnvironmentID      string
	Mode               string
	PersonaName        string
	PersonaAvatarURL   *string
	Capabilities       []string
	SessionGranularity string
	ReturnURL          string
}

// PublicationCredentials stores OAuth app credentials on a publication row.
type PublicationCredentials struct {
	ClientID            string
	ClientSecret        string
	WebhookSecret       string
	SigningSecret       *string
}

// IntegrationRepo persists integrations list/CRUD state.
type IntegrationRepo struct {
	db *sql.DB
}

// NewIntegrationRepo returns an integrations repository.
func NewIntegrationRepo(db *sql.DB) *IntegrationRepo {
	return &IntegrationRepo{db: db}
}

func installationsTable(p IntegrationProvider) string {
	return string(p) + "_installations"
}

func publicationsTable(p IntegrationProvider) string {
	return string(p) + "_publications"
}

var pendingPublicationStatuses = []string{
	"pending_setup",
	"credentials_filled",
	"awaiting_install",
}

// ListInstallationsByUser returns live installations for a user+provider.
func (r *IntegrationRepo) ListInstallationsByUser(
	ctx context.Context,
	userID string,
	provider IntegrationProvider,
) ([]IntegrationInstallation, error) {
	table := installationsTable(provider)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, provider_id, workspace_id,
			workspace_name, install_kind, app_id, bot_user_id, vault_id,
			created_at, revoked_at
		FROM %s
		WHERE user_id = ? AND provider_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC`, table),
		userID, string(provider),
	)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	defer rows.Close()
	return scanInstallations(rows)
}

// GetInstallation loads one installation by id.
func (r *IntegrationRepo) GetInstallation(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
) (*IntegrationInstallation, error) {
	table := installationsTable(provider)
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, provider_id, workspace_id,
			workspace_name, install_kind, app_id, bot_user_id, vault_id,
			created_at, revoked_at
		FROM %s WHERE id = ?`, table), id)
	item, err := scanInstallation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get installation: %w", err)
	}
	return item, nil
}

// ListPublicationsByInstallation lists publications for an installation.
func (r *IntegrationRepo) ListPublicationsByInstallation(
	ctx context.Context,
	provider IntegrationProvider,
	installationID string,
) ([]IntegrationPublication, error) {
	table := publicationsTable(provider)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, unpublished_at,
			client_id, client_secret_cipher, webhook_secret_cipher,
			signing_secret_cipher, vault_id, return_url
		FROM %s
		WHERE installation_id = ? AND unpublished_at IS NULL
		ORDER BY created_at DESC`, table), installationID)
	if err != nil {
		return nil, fmt.Errorf("list publications by installation: %w", err)
	}
	defer rows.Close()
	return scanPublications(rows)
}

// ListPublicationsByUserAndAgent lists live publications for user+agent.
func (r *IntegrationRepo) ListPublicationsByUserAndAgent(
	ctx context.Context,
	provider IntegrationProvider,
	userID, agentID string,
) ([]IntegrationPublication, error) {
	table := publicationsTable(provider)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, unpublished_at,
			client_id, client_secret_cipher, webhook_secret_cipher,
			signing_secret_cipher, vault_id, return_url
		FROM %s
		WHERE user_id = ? AND agent_id = ? AND unpublished_at IS NULL
		ORDER BY created_at DESC`, table),
		userID, agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list publications by agent: %w", err)
	}
	defer rows.Close()
	return scanPublications(rows)
}

// ListPendingPublicationsByUser lists in-progress publications.
func (r *IntegrationRepo) ListPendingPublicationsByUser(
	ctx context.Context,
	provider IntegrationProvider,
	userID string,
) ([]IntegrationPublication, error) {
	table := publicationsTable(provider)
	placeholders := strings.Repeat("?,", len(pendingPublicationStatuses)-1) + "?"
	args := make([]any, 0, len(pendingPublicationStatuses)+1)
	args = append(args, userID)
	for _, st := range pendingPublicationStatuses {
		args = append(args, st)
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, unpublished_at,
			client_id, client_secret_cipher, webhook_secret_cipher,
			signing_secret_cipher, vault_id, return_url
		FROM %s
		WHERE user_id = ? AND unpublished_at IS NULL
			AND status IN (%s)
		ORDER BY created_at DESC`, table, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("list pending publications: %w", err)
	}
	defer rows.Close()
	return scanPublications(rows)
}

// GetPublication loads one publication by id.
func (r *IntegrationRepo) GetPublication(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
) (*IntegrationPublication, error) {
	table := publicationsTable(provider)
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, unpublished_at,
			client_id, client_secret_cipher, webhook_secret_cipher,
			signing_secret_cipher, vault_id, return_url
		FROM %s WHERE id = ?`, table), id)
	item, err := scanPublication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get publication: %w", err)
	}
	return item, nil
}

// InsertPublicationShell creates a pending_setup publication row.
func (r *IntegrationRepo) InsertPublicationShell(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
	row NewPublicationShell,
) (*IntegrationPublication, error) {
	table := publicationsTable(provider)
	now := time.Now().Unix()
	caps, err := json.Marshal(row.Capabilities)
	if err != nil {
		return nil, err
	}
	mode := row.Mode
	if mode == "" {
		mode = "full"
	}
	gran := row.SessionGranularity
	if gran == "" {
		gran = defaultSessionGranularity(provider)
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, return_url
		) VALUES (?, ?, ?, ?, '', ?, ?, 'pending_setup', ?, ?, ?, ?, ?, ?)`,
		table),
		id, tenantOrDefault(row.TenantID), row.UserID, row.AgentID,
		nullIfEmpty(row.EnvironmentID), mode,
		row.PersonaName, row.PersonaAvatarURL, string(caps), gran, now,
		nullIfEmpty(row.ReturnURL),
	)
	if err != nil {
		return nil, fmt.Errorf("insert publication shell: %w", err)
	}
	return r.GetPublication(ctx, provider, id)
}

// SetPublicationCredentials stores credentials and moves to awaiting_install.
func (r *IntegrationRepo) SetPublicationCredentials(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
	creds PublicationCredentials,
) error {
	table := publicationsTable(provider)
	now := time.Now().Unix()
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET
			client_id = ?,
			client_secret_cipher = ?,
			webhook_secret_cipher = ?,
			signing_secret_cipher = ?,
			status = 'awaiting_install'
		WHERE id = ? AND unpublished_at IS NULL`, table),
		creds.ClientID, creds.ClientSecret, creds.WebhookSecret,
		creds.SigningSecret, id,
	)
	if err != nil {
		return fmt.Errorf("set publication credentials: %w", err)
	}
	_ = now
	return nil
}

// UpdatePublicationPersona patches persona fields.
func (r *IntegrationRepo) UpdatePublicationPersona(
	ctx context.Context,
	provider IntegrationProvider,
	id, name string,
	avatarURL *string,
) error {
	table := publicationsTable(provider)
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET persona_name = ?, persona_avatar_url = ?
		WHERE id = ?`, table), name, avatarURL, id)
	if err != nil {
		return fmt.Errorf("update publication persona: %w", err)
	}
	return nil
}

// UpdatePublicationCapabilities replaces capability set.
func (r *IntegrationRepo) UpdatePublicationCapabilities(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
	caps []string,
) error {
	table := publicationsTable(provider)
	raw, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET capabilities = ? WHERE id = ?`, table),
		string(raw), id,
	)
	if err != nil {
		return fmt.Errorf("update publication capabilities: %w", err)
	}
	return nil
}

// MarkPublicationUnpublished soft-unpublishes a publication.
func (r *IntegrationRepo) MarkPublicationUnpublished(
	ctx context.Context,
	provider IntegrationProvider,
	id string,
	at int64,
) error {
	table := publicationsTable(provider)
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET status = 'unpublished', unpublished_at = ?
		WHERE id = ?`, table), at, id)
	if err != nil {
		return fmt.Errorf("mark publication unpublished: %w", err)
	}
	return nil
}

// ListLinearDispatchRules lists rules for a publication.
func (r *IntegrationRepo) ListLinearDispatchRules(
	ctx context.Context,
	publicationID string,
) ([]LinearDispatchRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, publication_id, name, enabled,
			filter_label, filter_states, filter_project_id,
			max_concurrent, poll_interval_seconds, last_polled_at,
			created_at, updated_at
		FROM linear_dispatch_rules
		WHERE publication_id = ?
		ORDER BY created_at ASC`, publicationID)
	if err != nil {
		return nil, fmt.Errorf("list dispatch rules: %w", err)
	}
	defer rows.Close()
	out := make([]LinearDispatchRule, 0)
	for rows.Next() {
		item, err := scanDispatchRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetLinearDispatchRule loads one dispatch rule.
func (r *IntegrationRepo) GetLinearDispatchRule(
	ctx context.Context,
	id string,
) (*LinearDispatchRule, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, publication_id, name, enabled,
			filter_label, filter_states, filter_project_id,
			max_concurrent, poll_interval_seconds, last_polled_at,
			created_at, updated_at
		FROM linear_dispatch_rules WHERE id = ?`, id)
	item, err := scanDispatchRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dispatch rule: %w", err)
	}
	return item, nil
}

// InsertLinearDispatchRule creates a dispatch rule.
func (r *IntegrationRepo) InsertLinearDispatchRule(
	ctx context.Context,
	id string,
	row NewLinearDispatchRule,
) (*LinearDispatchRule, error) {
	now := time.Now().Unix()
	statesJSON, err := encodeStringSlice(row.FilterStates)
	if err != nil {
		return nil, err
	}
	enabled := 0
	if row.Enabled {
		enabled = 1
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO linear_dispatch_rules (
			id, tenant_id, publication_id, name, enabled,
			filter_label, filter_states, filter_project_id,
			max_concurrent, poll_interval_seconds, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantOrDefault(row.TenantID), row.PublicationID, row.Name,
		enabled, row.FilterLabel, statesJSON, row.FilterProjectID,
		row.MaxConcurrent, row.PollIntervalSeconds, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert dispatch rule: %w", err)
	}
	return r.GetLinearDispatchRule(ctx, id)
}

// UpdateLinearDispatchRule patches a dispatch rule.
func (r *IntegrationRepo) UpdateLinearDispatchRule(
	ctx context.Context,
	id string,
	patch LinearDispatchRulePatch,
) (*LinearDispatchRule, error) {
	existing, err := r.GetLinearDispatchRule(ctx, id)
	if err != nil || existing == nil {
		return existing, err
	}
	name := existing.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
	}
	enabled := existing.Enabled
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	filterLabel := existing.FilterLabel
	if patch.FilterLabel != nil {
		filterLabel = *patch.FilterLabel
	}
	filterStates := existing.FilterStates
	if patch.FilterStates != nil {
		filterStates = *patch.FilterStates
	}
	filterProject := existing.FilterProjectID
	if patch.FilterProjectID != nil {
		filterProject = *patch.FilterProjectID
	}
	maxConcurrent := existing.MaxConcurrent
	if patch.MaxConcurrent != nil {
		maxConcurrent = *patch.MaxConcurrent
	}
	pollInterval := existing.PollIntervalSeconds
	if patch.PollIntervalSeconds != nil {
		pollInterval = *patch.PollIntervalSeconds
	}
	statesJSON, err := encodeStringSlice(filterStates)
	if err != nil {
		return nil, err
	}
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	now := time.Now().Unix()
	_, err = r.db.ExecContext(ctx, `
		UPDATE linear_dispatch_rules SET
			name = ?, enabled = ?, filter_label = ?, filter_states = ?,
			filter_project_id = ?, max_concurrent = ?,
			poll_interval_seconds = ?, updated_at = ?
		WHERE id = ?`,
		name, enabledInt, filterLabel, statesJSON, filterProject,
		maxConcurrent, pollInterval, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update dispatch rule: %w", err)
	}
	return r.GetLinearDispatchRule(ctx, id)
}

// DeleteLinearDispatchRule removes a dispatch rule.
func (r *IntegrationRepo) DeleteLinearDispatchRule(
	ctx context.Context,
	id string,
) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM linear_dispatch_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete dispatch rule: %w", err)
	}
	return nil
}

func defaultSessionGranularity(p IntegrationProvider) string {
	if p == ProviderSlack {
		return "per_thread"
	}
	return "per_issue"
}

func encodeStringSlice(items []string) (any, error) {
	if items == nil {
		return nil, nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

func decodeStringSlice(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	return out, nil
}

type installationScanner interface {
	Scan(dest ...any) error
}

func scanInstallation(row installationScanner) (*IntegrationInstallation, error) {
	var item IntegrationInstallation
	var appID, vaultID sql.NullString
	var revokedAt sql.NullInt64
	err := row.Scan(
		&item.ID, &item.TenantID, &item.UserID, &item.ProviderID,
		&item.WorkspaceID, &item.WorkspaceName, &item.InstallKind,
		&appID, &item.BotUserID, &vaultID, &item.CreatedAt, &revokedAt,
	)
	if err != nil {
		return nil, err
	}
	if appID.Valid {
		item.AppID = &appID.String
	}
	if vaultID.Valid {
		item.VaultID = &vaultID.String
	}
	if revokedAt.Valid {
		item.RevokedAt = &revokedAt.Int64
	}
	return &item, nil
}

func scanInstallations(rows *sql.Rows) ([]IntegrationInstallation, error) {
	out := make([]IntegrationInstallation, 0)
	for rows.Next() {
		item, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

type publicationScanner interface {
	Scan(dest ...any) error
}

func scanPublication(row publicationScanner) (*IntegrationPublication, error) {
	var item IntegrationPublication
	var envID, avatar, clientID, clientSecret, webhook, signing, vaultID sql.NullString
	var returnURL sql.NullString
	var unpublishedAt sql.NullInt64
	var capsRaw string
	err := row.Scan(
		&item.ID, &item.TenantID, &item.UserID, &item.AgentID,
		&item.InstallationID, &envID, &item.Mode, &item.Status,
		&item.PersonaName, &avatar, &capsRaw, &item.SessionGranularity,
		&item.CreatedAt, &unpublishedAt, &clientID, &clientSecret,
		&webhook, &signing, &vaultID, &returnURL,
	)
	if err != nil {
		return nil, err
	}
	if envID.Valid {
		item.EnvironmentID = &envID.String
	}
	if avatar.Valid {
		item.PersonaAvatarURL = &avatar.String
	}
	if err := json.Unmarshal([]byte(capsRaw), &item.Capabilities); err != nil {
		item.Capabilities = []string{}
	}
	if unpublishedAt.Valid {
		item.UnpublishedAt = &unpublishedAt.Int64
	}
	if clientID.Valid {
		item.ClientID = &clientID.String
	}
	if clientSecret.Valid {
		item.ClientSecretCipher = &clientSecret.String
	}
	if webhook.Valid {
		item.WebhookSecretCipher = &webhook.String
	}
	if signing.Valid {
		item.SigningSecretCipher = &signing.String
	}
	if vaultID.Valid {
		item.VaultID = &vaultID.String
	}
	if returnURL.Valid {
		item.ReturnURL = &returnURL.String
	}
	return &item, nil
}

func scanPublications(rows *sql.Rows) ([]IntegrationPublication, error) {
	out := make([]IntegrationPublication, 0)
	for rows.Next() {
		item, err := scanPublication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func scanDispatchRule(row publicationScanner) (*LinearDispatchRule, error) {
	var item LinearDispatchRule
	var filterLabel, filterProject sql.NullString
	var filterStates sql.NullString
	var lastPolled sql.NullInt64
	var enabled int
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PublicationID, &item.Name,
		&enabled, &filterLabel, &filterStates, &filterProject,
		&item.MaxConcurrent, &item.PollIntervalSeconds, &lastPolled,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.Enabled = enabled != 0
	if filterLabel.Valid {
		item.FilterLabel = &filterLabel.String
	}
	if filterProject.Valid {
		item.FilterProjectID = &filterProject.String
	}
	states, err := decodeStringSlice(filterStates)
	if err != nil {
		return nil, err
	}
	item.FilterStates = states
	if lastPolled.Valid {
		item.LastPolledAt = &lastPolled.Int64
	}
	return &item, nil
}
