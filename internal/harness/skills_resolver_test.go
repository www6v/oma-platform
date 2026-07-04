package harness_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestResolveSkillsForTurnCustomSkill(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	skillFiles := store.NewSkillFileStore(t.TempDir())
	skills := store.NewSkillRepo(db.DB, skillFiles)

	skill, ver, err := skills.Create(ctx, store.CreateSkillInput{
		TenantID:     "default",
		DisplayTitle: "Incident Runbooks",
		Name:         "incident-runbooks",
		Description:  "Consult runbooks before infra changes",
		Files: []store.SkillFileInput{
			{
				Filename: "SKILL.md",
				Content:  "# Runbooks\nConsult runbooks before touching infrastructure.",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &harness.ResourceResolver{
		Skills:     skills,
		SkillFiles: skillFiles,
	}
	agentSkills, err := json.Marshal([]any{
		map[string]any{
			"type":     "custom",
			"skill_id": skill.ID,
			"version":  ver.Version,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.ResolveSkillsForTurn(ctx, "default", agentSkills)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("skills len=%d want 1", len(got))
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0], &payload); err != nil {
		t.Fatal(err)
	}
	addition, _ := payload["system_prompt_addition"].(string)
	if addition == "" || !strings.Contains(addition, "Runbooks") {
		t.Fatalf("addition=%q", addition)
	}
	files, _ := payload["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files=%v", payload["files"])
	}
}

func TestResolveSkillsForTurnBuiltin(t *testing.T) {
	resolver := &harness.ResourceResolver{}
	agentSkills := json.RawMessage(
		`[{"type":"anthropic","skill_id":"builtin_pdf"}]`,
	)
	got, err := resolver.ResolveSkillsForTurn(
		context.Background(), "default", agentSkills,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("skills len=%d want 1", len(got))
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0], &payload); err != nil {
		t.Fatal(err)
	}
	addition, _ := payload["system_prompt_addition"].(string)
	if addition == "" || !strings.Contains(addition, "PDF") {
		t.Fatalf("addition=%q", addition)
	}
}
