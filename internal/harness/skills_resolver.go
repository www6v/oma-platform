package harness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

const skillMountRoot = "home/user/.skills"

// ResolveSkillsForTurn resolves agent.skills into harness-ready payloads
// with inlined SKILL.md and file bytes for sandbox mounting (AMA-aligned).
func (r *ResourceResolver) ResolveSkillsForTurn(
	ctx context.Context,
	tenantID string,
	agentSkills json.RawMessage,
) ([]json.RawMessage, error) {
	specs := parseSkillSpecs(agentSkills)
	if len(specs) == 0 || r == nil {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(specs))
	for _, spec := range specs {
		mounted, err := r.resolveOneSkill(ctx, tenantID, spec)
		if err != nil || mounted == nil {
			continue
		}
		raw, err := json.Marshal(mounted)
		if err != nil {
			continue
		}
		out = append(out, raw)
	}
	return out, nil
}

func parseSkillSpecs(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []any
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if skillID, _ := m["skill_id"].(string); skillID != "" {
			out = append(out, m)
		}
	}
	return out
}

func (r *ResourceResolver) resolveOneSkill(
	ctx context.Context,
	tenantID string,
	spec map[string]any,
) (map[string]any, error) {
	skillID, _ := spec["skill_id"].(string)
	if skillID == "" {
		return nil, fmt.Errorf("skill missing skill_id")
	}

	if store.IsBuiltinSkillID(skillID) {
		return resolveBuiltinSkill(skillID), nil
	}
	if r.Skills == nil || r.SkillFiles == nil {
		return nil, fmt.Errorf("skill repos unavailable")
	}

	meta, err := r.Skills.Get(ctx, tenantID, skillID)
	if err != nil || meta == nil {
		return nil, fmt.Errorf("skill %s not found", skillID)
	}

	version, _ := spec["version"].(string)
	if version == "" || version == "latest" {
		version = meta.LatestVersion
	}
	if version == "" {
		return nil, fmt.Errorf("skill %s missing version", skillID)
	}

	ver, err := r.Skills.GetVersion(ctx, tenantID, skillID, version)
	if err != nil || ver == nil {
		return nil, fmt.Errorf("skill version %s/%s not found", skillID, version)
	}

	fileRows, err := r.SkillFiles.ReadVersionFiles(
		tenantID, skillID, version, ver.Files,
	)
	if err != nil {
		return nil, err
	}

	displayName := meta.DisplayTitle
	if displayName == "" {
		displayName = meta.Name
	}
	if displayName == "" {
		displayName = skillID
	}
	mountName := meta.Name
	if mountName == "" {
		mountName = skillID
	}

	skillBody := ""
	filesOut := make([]map[string]any, 0, len(fileRows))
	for _, row := range fileRows {
		filename := row["filename"]
		content := row["content"]
		encoding := row["encoding"]
		if filename == "" {
			continue
		}
		var data []byte
		if encoding == "base64" {
			decoded, decErr := base64.StdEncoding.DecodeString(content)
			if decErr != nil {
				continue
			}
			data = decoded
		} else {
			data = []byte(content)
		}
		if filename == "SKILL.md" && skillBody == "" {
			skillBody = string(data)
		}
		filesOut = append(filesOut, map[string]any{
			"filename":       filename,
			"content_base64": base64.StdEncoding.EncodeToString(data),
		})
	}

	addition := skillPromptAddition(displayName, meta.Description, skillBody, mountName)

	return map[string]any{
		"type":                   "skill",
		"skill_id":               skillID,
		"name":                   mountName,
		"display_name":           displayName,
		"source":                 meta.Source,
		"version":                version,
		"mount_root":             fmt.Sprintf("/home/user/.skills/%s", mountName),
		"system_prompt_addition": addition,
		"files":                  filesOut,
	}, nil
}

func resolveBuiltinSkill(skillID string) map[string]any {
	builtin := store.BuiltinSkillByID(skillID)
	if builtin == nil {
		return nil
	}
	name := builtin.Name
	if name == "" {
		name = skillID
	}
	displayName := builtin.DisplayTitle
	if displayName == "" {
		displayName = name
	}
	addition := skillPromptAddition(
		displayName,
		builtin.Description,
		"",
		name,
	)
	return map[string]any{
		"type":                   "skill",
		"skill_id":               skillID,
		"name":                   name,
		"display_name":           displayName,
		"source":                 "anthropic",
		"mount_root":             fmt.Sprintf("/home/user/.skills/%s", name),
		"system_prompt_addition": addition,
		"files":                  []any{},
	}
}

func skillPromptAddition(
	displayName, description, skillBody, mountName string,
) string {
	if skillBody != "" {
		return fmt.Sprintf(
			"<skill name=%q>\n%s\n</skill>",
			displayName,
			skillBody,
		)
	}
	desc := description
	if desc == "" {
		desc = "See SKILL.md for instructions."
	}
	return fmt.Sprintf(
		"[Skill: %s] %s Read /home/user/.skills/%s/SKILL.md for instructions.",
		displayName,
		desc,
		mountName,
	)
}
