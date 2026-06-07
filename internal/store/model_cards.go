package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ModelCard is a persisted model credential row (no plaintext api key).
type ModelCard struct {
	ID             string
	TenantID       string
	ModelID        string
	Model          string
	Provider       string
	BaseURL        string
	CustomHeaders  json.RawMessage
	APIKeyPreview  string
	IsDefault      bool
	CreatedAt      int64
	UpdatedAt      *int64
	ArchivedAt     *int64
}

// CreateModelCardInput holds fields for a new model card.
type CreateModelCardInput struct {
	TenantID       string
	ModelID        string
	Model          string
	Provider       string
	APIKey         string
	BaseURL        string
	CustomHeaders  json.RawMessage
	MakeDefault    bool
}

// UpdateModelCardInput holds patch fields for Update.
type UpdateModelCardInput struct {
	ModelID        *string
	Model          *string
	Provider       *string
	APIKey         *string
	BaseURL        *string
	BaseURLSet     bool
	CustomHeaders  json.RawMessage
	CustomSet      bool
	IsDefault      *bool
}

// ModelCardRepo persists model cards in SQLite.
type ModelCardRepo struct {
	db *sql.DB
}

// NewModelCardRepo returns a model card repository.
func NewModelCardRepo(db *sql.DB) *ModelCardRepo {
	return &ModelCardRepo{db: db}
}

// Create inserts a model card.
func (r *ModelCardRepo) Create(
	ctx context.Context,
	input CreateModelCardInput,
) (*ModelCard, error) {
	tenantID := tenantOrDefault(input.TenantID)
	model := input.Model
	if model == "" {
		model = input.ModelID
	}
	id := generateModelCardID()
	now := time.Now().UnixMilli()
	cipher := encodeAPIKey(input.APIKey)
	preview := apiKeyPreview(input.APIKey)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if input.MakeDefault {
		if err := clearDefaultModelCards(ctx, tx, tenantID, now); err != nil {
			return nil, err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO model_cards (
			id, tenant_id, model_id, model, provider, base_url,
			custom_headers, api_key_cipher, api_key_preview, is_default,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, input.ModelID, model, input.Provider,
		nullIfEmpty(input.BaseURL), nullableJSON(input.CustomHeaders),
		cipher, preview, boolToInt(input.MakeDefault), now, now,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("insert model card: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads a model card by id.
func (r *ModelCardRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*ModelCard, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, model_id, model, provider, base_url, custom_headers,
			api_key_preview, is_default, created_at, updated_at, archived_at
		FROM model_cards
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	return scanModelCard(row)
}

// GetByModelID loads a card by tenant-unique handle.
func (r *ModelCardRepo) GetByModelID(
	ctx context.Context,
	tenantID, modelID string,
) (*ModelCard, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, model_id, model, provider, base_url, custom_headers,
			api_key_preview, is_default, created_at, updated_at, archived_at
		FROM model_cards
		WHERE model_id = ? AND tenant_id = ? AND archived_at IS NULL`,
		modelID, tenantOrDefault(tenantID),
	)
	return scanModelCard(row)
}

// GetDefault loads the tenant default card if set.
func (r *ModelCardRepo) GetDefault(
	ctx context.Context,
	tenantID string,
) (*ModelCard, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, model_id, model, provider, base_url, custom_headers,
			api_key_preview, is_default, created_at, updated_at, archived_at
		FROM model_cards
		WHERE tenant_id = ? AND is_default = 1 AND archived_at IS NULL
		LIMIT 1`,
		tenantOrDefault(tenantID),
	)
	return scanModelCard(row)
}

// List returns model cards for a tenant.
func (r *ModelCardRepo) List(
	ctx context.Context,
	tenantID string,
	includeArchived bool,
) ([]*ModelCard, error) {
	query := `
		SELECT id, model_id, model, provider, base_url, custom_headers,
			api_key_preview, is_default, created_at, updated_at, archived_at
		FROM model_cards
		WHERE tenant_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantOrDefault(tenantID))
	if err != nil {
		return nil, fmt.Errorf("list model cards: %w", err)
	}
	defer rows.Close()

	var out []*ModelCard
	for rows.Next() {
		card, err := scanModelCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, rows.Err()
}

// Update patches a model card.
func (r *ModelCardRepo) Update(
	ctx context.Context,
	tenantID, id string,
	input UpdateModelCardInput,
) (*ModelCard, error) {
	current, err := r.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}
	if current.ArchivedAt != nil {
		return nil, ErrArchived
	}

	modelID := current.ModelID
	if input.ModelID != nil {
		modelID = *input.ModelID
	}
	model := current.Model
	if input.Model != nil {
		model = *input.Model
	}
	provider := current.Provider
	if input.Provider != nil {
		provider = *input.Provider
	}
	baseURL := current.BaseURL
	if input.BaseURLSet {
		if input.BaseURL != nil {
			baseURL = *input.BaseURL
		} else {
			baseURL = ""
		}
	}
	headers := current.CustomHeaders
	if input.CustomSet {
		headers = input.CustomHeaders
	}
	isDefault := current.IsDefault
	if input.IsDefault != nil {
		isDefault = *input.IsDefault
	}

	now := time.Now().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if isDefault {
		if err := clearDefaultModelCards(ctx, tx, tenantID, now); err != nil {
			return nil, err
		}
	}

	query := `
		UPDATE model_cards
		SET model_id = ?, model = ?, provider = ?, base_url = ?,
			custom_headers = ?, is_default = ?, updated_at = ?`
	args := []any{
		modelID, model, provider, nullIfEmpty(baseURL),
		nullableJSON(headers), boolToInt(isDefault), now,
	}
	if input.APIKey != nil {
		query += `, api_key_cipher = ?, api_key_preview = ?`
		args = append(args, encodeAPIKey(*input.APIKey), apiKeyPreview(*input.APIKey))
	}
	query += ` WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`
	args = append(args, id, tenantOrDefault(tenantID))

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("update model card: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, id)
}

// Delete hard-deletes a model card.
func (r *ModelCardRepo) Delete(ctx context.Context, tenantID, id string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM model_cards
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return fmt.Errorf("delete model card: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetAPIKey returns the stored api key for a card id.
func (r *ModelCardRepo) GetAPIKey(
	ctx context.Context,
	tenantID, id string,
) (string, error) {
	var cipher string
	err := r.db.QueryRowContext(ctx, `
		SELECT api_key_cipher FROM model_cards
		WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`,
		id, tenantOrDefault(tenantID),
	).Scan(&cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get api key: %w", err)
	}
	return decodeAPIKey(cipher), nil
}

func scanModelCard(row interface {
	Scan(dest ...any) error
}) (*ModelCard, error) {
	var (
		card       ModelCard
		baseURL    sql.NullString
		headers    sql.NullString
		isDefault  int
		updatedAt  sql.NullInt64
		archivedAt sql.NullInt64
	)
	if err := row.Scan(
		&card.ID, &card.ModelID, &card.Model, &card.Provider, &baseURL,
		&headers, &card.APIKeyPreview, &isDefault,
		&card.CreatedAt, &updatedAt, &archivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan model card: %w", err)
	}
	card.TenantID = defaultTenantID
	if baseURL.Valid {
		card.BaseURL = baseURL.String
	}
	if headers.Valid && headers.String != "" {
		card.CustomHeaders = json.RawMessage(headers.String)
	}
	card.IsDefault = isDefault == 1
	if updatedAt.Valid {
		v := updatedAt.Int64
		card.UpdatedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Int64
		card.ArchivedAt = &v
	}
	return &card, nil
}

func clearDefaultModelCards(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	now int64,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE model_cards
		SET is_default = 0, updated_at = ?
		WHERE tenant_id = ? AND is_default = 1`,
		now, tenantID,
	)
	return err
}

func encodeAPIKey(key string) string {
	return base64.StdEncoding.EncodeToString([]byte(key))
}

func decodeAPIKey(cipher string) string {
	raw, err := base64.StdEncoding.DecodeString(cipher)
	if err != nil {
		return ""
	}
	return string(raw)
}

func apiKeyPreview(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}

func generateModelCardID() string {
	return "mcard-" + randomString(idLength)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
