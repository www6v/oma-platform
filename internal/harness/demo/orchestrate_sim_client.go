package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// OrchestrateRepoMountPath is the cookbook zip mount on session create.
	OrchestrateRepoMountPath = "repo.zip"
	// OrchestrateTurn1Marker ends turn 1 after the full issue→PR chain.
	OrchestrateTurn1Marker = "orchestrate-cookbook-turn-1-ok"
	// OrchestrateVerifyMarker ends turn 2 after verifying merged PR state.
	OrchestrateVerifyMarker = "orchestrate-cookbook-verify-ok"
	// OrchestratePRStateRelPath is the mock gh persisted PR state file.
	OrchestratePRStateRelPath = "mnt/user/.gh-state/pr_101.json"
)

// OrchestrateSimulatingClient validates zip fixture mounts and multi-turn
// issue→PR chain persistence for CMA_orchestrate_issue_to_pr.
type OrchestrateSimulatingClient struct {
	harness.RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements harness.Client.
func (c *OrchestrateSimulatingClient) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	return harness.TurnResponse{Events: events}, err
}

// RunTurnStream implements harness.StreamingClient.
func (c *OrchestrateSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	c.mu.Lock()
	c.turns++
	turnNum := c.turns
	c.mu.Unlock()

	c.RecordRequest(req)

	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if len(req.Resources) == 0 {
		return fmt.Errorf("expected session resources on turn request")
	}

	text, err := c.messageForTurn(turnNum, req)
	if err != nil {
		return err
	}
	return emitOrchestrateMessage(onEvent, text)
}

// TurnCount returns how many harness turns ran.
func (c *OrchestrateSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}

func (c *OrchestrateSimulatingClient) messageForTurn(
	turnNum int,
	req harness.TurnRequest,
) (string, error) {
	switch turnNum {
	case 1:
		if err := validateOrchestrateZipResource(req.Resources); err != nil {
			return "", err
		}
		if err := validateOrchestrateEnvironment(req.Environment); err != nil {
			return "", err
		}
		if err := writeMergedPRState(req.Workdir); err != nil {
			return "", err
		}
		return OrchestrateTurn1Marker + ": issue #42 fixed, PR #101 merged", nil
	case 2:
		state, err := readPRState(req.Workdir)
		if err != nil {
			return "", err
		}
		if err := validateMergedPRState(state); err != nil {
			return "", err
		}
		return OrchestrateVerifyMarker + ": " + state, nil
	default:
		return "", fmt.Errorf("unexpected orchestrate turn %d", turnNum)
	}
}

func validateOrchestrateZipResource(resources []json.RawMessage) error {
	data, ok := fileBytesAtMount(resources, OrchestrateRepoMountPath)
	if !ok {
		return fmt.Errorf(
			"expected file resource at mount_path %q",
			OrchestrateRepoMountPath,
		)
	}
	required := []string{
		"gh-mock",
		"issue_42.json",
		"src/url_utils.py",
		"tests/test_urls.py",
	}
	for _, path := range required {
		if !zipContainsPath(data, path) {
			return fmt.Errorf("repo.zip missing %q", path)
		}
	}
	return nil
}

func validateOrchestrateEnvironment(env json.RawMessage) error {
	if len(env) == 0 {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(env, &root) != nil {
		return nil
	}
	cfg, _ := root["config"].(map[string]any)
	if cfg == nil {
		return nil
	}
	pkgs, _ := cfg["packages"].(map[string]any)
	if pkgs == nil {
		return nil
	}
	pip, _ := pkgs["pip"].([]any)
	for _, item := range pip {
		if s, ok := item.(string); ok && s == "pytest" {
			return nil
		}
	}
	return fmt.Errorf("expected pytest in environment packages.pip")
}

func writeMergedPRState(workdir string) error {
	state := map[string]any{
		"number": 101,
		"state":  "merged",
		"checks": map[string]string{"ci/test": "pass"},
		"reviews": []map[string]string{
			{
				"author": "reviewer-bot",
				"state":  "APPROVED",
				"body":   "LGTM",
			},
		},
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(workdir, filepath.FromSlash(OrchestratePRStateRelPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o644)
}

func readPRState(workdir string) (string, error) {
	target := filepath.Join(workdir, filepath.FromSlash(OrchestratePRStateRelPath))
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("read pr state: %w", err)
	}
	return string(data), nil
}

func validateMergedPRState(raw string) error {
	var state map[string]any
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return fmt.Errorf("parse pr state: %w", err)
	}
	if state["state"] != "merged" {
		return fmt.Errorf("pr state=%v want merged", state["state"])
	}
	checks, _ := state["checks"].(map[string]any)
	if checks == nil || checks["ci/test"] != "pass" {
		return fmt.Errorf("ci/test not passing in pr state")
	}
	reviews, _ := state["reviews"].([]any)
	approved := false
	for _, item := range reviews {
		rev, _ := item.(map[string]any)
		if rev["state"] == "APPROVED" {
			approved = true
		}
	}
	if !approved {
		return fmt.Errorf("pr state missing approving review")
	}
	return nil
}

func emitOrchestrateMessage(
	onEvent harness.EventHandler,
	text string,
) error {
	raw, err := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	if err != nil {
		return err
	}
	return onEvent(raw)
}
