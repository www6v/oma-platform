package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var skillNameRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Skill is a tenant-owned or built-in skill metadata row.
type Skill struct {
	ID            string
	TenantID      string
	DisplayTitle  string
	Name          string
	Description   string
	Source        string
	LatestVersion string
	CreatedAt     int64
	UpdatedAt     *int64
}

// SkillVersion is one immutable skill version manifest.
type SkillVersion struct {
	SkillID   string
	TenantID  string
	Version   string
	Files     []SkillFileEntry
	CreatedAt int64
}

// CreateSkillInput holds fields for a new custom skill.
type CreateSkillInput struct {
	TenantID     string
	DisplayTitle string
	Name         string
	Description  string
	Files        []SkillFileInput
}

// CreateSkillVersionInput holds fields for a new skill version.
type CreateSkillVersionInput struct {
	TenantID     string
	SkillID      string
	DisplayTitle string
	Description  string
	Files        []SkillFileInput
}

// SkillRepo persists custom skills in SQLite.
type SkillRepo struct {
	db    *sql.DB
	files *SkillFileStore
}

// NewSkillRepo returns a skill repository.
func NewSkillRepo(db *sql.DB, files *SkillFileStore) *SkillRepo {
	return &SkillRepo{db: db, files: files}
}

// Create inserts a custom skill and its first version.
func (r *SkillRepo) Create(
	ctx context.Context,
	input CreateSkillInput,
) (*Skill, *SkillVersion, error) {
	tenantID := tenantOrDefault(input.TenantID)
	if len(input.Files) == 0 {
		return nil, nil, fmt.Errorf("files array is required")
	}
	name := input.Name
	if name == "" {
		name = extractSkillNameFromFiles(input.Files)
	}
	if name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if !skillNameRE.MatchString(name) {
		return nil, nil, fmt.Errorf(
			"name must be lowercase letters, numbers, and hyphens only",
		)
	}
	displayTitle := input.DisplayTitle
	if displayTitle == "" {
		displayTitle = name
	}
	description := input.Description
	if description == "" {
		description = extractSkillDescFromFiles(input.Files)
	}

	id := "skill_" + randomString(idLength)
	version := fmt.Sprintf("%d", time.Now().UnixMilli())
	now := time.Now().UnixMilli()

	manifest, err := r.files.WriteVersionFiles(
		tenantID, id, version, input.Files,
	)
	if err != nil {
		return nil, nil, err
	}
	filesJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO skills (
			id, tenant_id, display_title, name, description, source,
			latest_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'custom', ?, ?, ?)`,
		id, tenantID, displayTitle, name, description, version, now, now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("insert skill: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO skill_versions (
			skill_id, tenant_id, version, files_json, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		id, tenantID, version, string(filesJSON), now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("insert skill version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	skill, err := r.Get(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	ver, err := r.GetVersion(ctx, tenantID, id, version)
	return skill, ver, err
}

// CreateVersion inserts a new version for an existing custom skill.
func (r *SkillRepo) CreateVersion(
	ctx context.Context,
	input CreateSkillVersionInput,
) (*Skill, *SkillVersion, error) {
	tenantID := tenantOrDefault(input.TenantID)
	if len(input.Files) == 0 {
		return nil, nil, fmt.Errorf("files array is required")
	}
	skill, err := r.Get(ctx, tenantID, input.SkillID)
	if err != nil {
		return nil, nil, err
	}
	if skill == nil {
		return nil, nil, ErrNotFound
	}
	if skill.Source != "custom" {
		return nil, nil, fmt.Errorf(
			"cannot create versions for built-in skills",
		)
	}

	version := fmt.Sprintf("%d", time.Now().UnixMilli())
	now := time.Now().UnixMilli()

	manifest, err := r.files.WriteVersionFiles(
		tenantID, input.SkillID, version, input.Files,
	)
	if err != nil {
		return nil, nil, err
	}
	filesJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, nil, err
	}

	displayTitle := input.DisplayTitle
	description := input.Description
	if displayTitle == "" {
		if name := extractSkillNameFromFiles(input.Files); name != "" {
			displayTitle = name
		}
	}
	if description == "" {
		description = extractSkillDescFromFiles(input.Files)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	updateSQL := `
		UPDATE skills
		SET latest_version = ?, updated_at = ?`
	args := []any{version, now}
	if displayTitle != "" {
		updateSQL += `, display_title = ?`
		args = append(args, displayTitle)
	}
	if description != "" {
		updateSQL += `, description = ?`
		args = append(args, description)
	}
	updateSQL += ` WHERE id = ? AND tenant_id = ?`
	args = append(args, input.SkillID, tenantID)

	_, err = tx.ExecContext(ctx, updateSQL, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("update skill: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO skill_versions (
			skill_id, tenant_id, version, files_json, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		input.SkillID, tenantID, version, string(filesJSON), now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("insert skill version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	updated, err := r.Get(ctx, tenantID, input.SkillID)
	if err != nil {
		return nil, nil, err
	}
	ver, err := r.GetVersion(ctx, tenantID, input.SkillID, version)
	return updated, ver, err
}

// Get loads one custom skill by id.
func (r *SkillRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*Skill, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, display_title, name, description, source,
			latest_version, created_at, updated_at
		FROM skills
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	return scanSkill(row, tenantOrDefault(tenantID))
}

// ListCustom returns all custom skills for a tenant.
func (r *SkillRepo) ListCustom(
	ctx context.Context,
	tenantID string,
) ([]*Skill, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, display_title, name, description, source,
			latest_version, created_at, updated_at
		FROM skills
		WHERE tenant_id = ?
		ORDER BY created_at DESC`,
		tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	var out []*Skill
	for rows.Next() {
		skill, err := scanSkill(rows, tenantOrDefault(tenantID))
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	return out, rows.Err()
}

// Delete removes a custom skill, all versions, and files.
func (r *SkillRepo) Delete(
	ctx context.Context,
	tenantID, id string,
) error {
	if IsBuiltinSkillID(id) {
		return fmt.Errorf("cannot delete built-in skill")
	}
	tenantID = tenantOrDefault(tenantID)
	versions, err := r.ListVersionSummaries(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		cur, err := r.Get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrNotFound
		}
	}

	for _, summary := range versions {
		ver, err := r.GetVersion(ctx, tenantID, id, summary.Version)
		if err != nil {
			return err
		}
		if ver != nil {
			_ = r.files.DeleteVersionFiles(
				tenantID, id, ver.Version, ver.Files,
			)
		}
	}
	_ = r.files.DeleteSkillDir(tenantID, id)

	res, err := r.db.ExecContext(ctx, `
		DELETE FROM skills WHERE id = ? AND tenant_id = ?`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, _ = r.db.ExecContext(ctx, `
		DELETE FROM skill_versions WHERE skill_id = ? AND tenant_id = ?`,
		id, tenantID,
	)
	return nil
}

// CountCustom returns custom skill rows for a tenant.
func (r *SkillRepo) CountCustom(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM skills WHERE tenant_id = ?`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count skills: %w", err)
	}
	return n, nil
}

// ListVersionSummaries returns version metadata without file bodies.
func (r *SkillRepo) ListVersionSummaries(
	ctx context.Context,
	tenantID, skillID string,
) ([]SkillVersionSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT version, files_json, created_at
		FROM skill_versions
		WHERE skill_id = ? AND tenant_id = ?
		ORDER BY CAST(version AS INTEGER) DESC`,
		skillID, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("list skill versions: %w", err)
	}
	defer rows.Close()

	var out []SkillVersionSummary
	for rows.Next() {
		var (
			version   string
			filesJSON string
			createdAt int64
		)
		if err := rows.Scan(&version, &filesJSON, &createdAt); err != nil {
			return nil, err
		}
		var files []SkillFileEntry
		_ = json.Unmarshal([]byte(filesJSON), &files)
		out = append(out, SkillVersionSummary{
			Version:    version,
			FileCount:  len(files),
			CreatedAt:  createdAt,
		})
	}
	return out, rows.Err()
}

// SkillVersionSummary is a lightweight version list row.
type SkillVersionSummary struct {
	Version   string
	FileCount int
	CreatedAt int64
}

// GetVersion loads one skill version manifest.
func (r *SkillRepo) GetVersion(
	ctx context.Context,
	tenantID, skillID, version string,
) (*SkillVersion, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT version, files_json, created_at
		FROM skill_versions
		WHERE skill_id = ? AND tenant_id = ? AND version = ?`,
		skillID, tenantOrDefault(tenantID), version,
	)
	var (
		ver       string
		filesJSON string
		createdAt int64
	)
	if err := row.Scan(&ver, &filesJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get skill version: %w", err)
	}
	var files []SkillFileEntry
	if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
		return nil, err
	}
	return &SkillVersion{
		SkillID:   skillID,
		TenantID:  tenantOrDefault(tenantID),
		Version:   ver,
		Files:     files,
		CreatedAt: createdAt,
	}, nil
}

func scanSkill(row interface {
	Scan(dest ...any) error
}, tenantID string,
) (*Skill, error) {
	var (
		id            string
		displayTitle  string
		name          string
		description   string
		source        string
		latestVersion string
		createdAt     int64
		updatedAt     sql.NullInt64
	)
	if err := row.Scan(
		&id, &displayTitle, &name, &description, &source,
		&latestVersion, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	skill := &Skill{
		ID:            id,
		TenantID:      tenantID,
		DisplayTitle:  displayTitle,
		Name:          name,
		Description:   description,
		Source:        source,
		LatestVersion: latestVersion,
		CreatedAt:     createdAt,
	}
	if updatedAt.Valid {
		v := updatedAt.Int64
		skill.UpdatedAt = &v
	}
	return skill, nil
}

func extractSkillNameFromFiles(files []SkillFileInput) string {
	for _, file := range files {
		if file.Filename != "SKILL.md" &&
			file.Filename != "skill.md" {
			continue
		}
		meta := parseSkillFrontmatter(file.Content)
		return meta["name"]
	}
	return ""
}

func extractSkillDescFromFiles(files []SkillFileInput) string {
	for _, file := range files {
		if file.Filename != "SKILL.md" &&
			file.Filename != "skill.md" {
			continue
		}
		meta := parseSkillFrontmatter(file.Content)
		return meta["description"]
	}
	return ""
}

func parseSkillFrontmatter(content string) map[string]string {
	out := map[string]string{}
	lines := splitLines(content)
	if len(lines) < 3 || lines[0] != "---" {
		return out
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			break
		}
		parts := splitKV(lines[i])
		if len(parts) == 2 {
			out[parts[0]] = parts[1]
		}
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func splitKV(line string) []string {
	idx := -1
	for i, ch := range line {
		if ch == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	key := trimSpace(line[:idx])
	val := trimSpace(line[idx+1:])
	val = trimQuotes(val)
	return []string{key, val}
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
