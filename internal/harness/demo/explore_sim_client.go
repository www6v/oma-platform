package demo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// ExploreRepoMountPath is the cookbook zip mount path on session create.
	ExploreRepoMountPath = "repo.zip"
	// ExploreDeployHistoryMountPath is added mid-session via resources.add.
	ExploreDeployHistoryMountPath = "DEPLOY_HISTORY.md"
	// ExploreArchitectureMarker ends turn 1 after grounded architecture answer.
	ExploreArchitectureMarker = "explore-cookbook-architecture-ok"
	// ExploreNotesMarker ends turn 2 simulating /tmp/NOTES.md output.
	ExploreNotesMarker = "explore-cookbook-notes-ok"
	// ExploreDeployMarker ends turn 3 after reading deploy history.
	ExploreDeployMarker = "explore-cookbook-deploy-history-ok"
)

// ExploreSimulatingClient validates zip + mid-session file resources for the
// explore unfamiliar codebase cookbook.
type ExploreSimulatingClient struct {
	harness.RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements harness.Client.
func (c *ExploreSimulatingClient) RunTurn(
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
func (c *ExploreSimulatingClient) RunTurnStream(
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
	return emitExploreMessage(onEvent, text)
}

func (c *ExploreSimulatingClient) messageForTurn(
	turnNum int,
	req harness.TurnRequest,
) (string, error) {
	switch turnNum {
	case 1:
		if err := validateRepoZipResource(req.Resources); err != nil {
			return "", err
		}
		return ExploreArchitectureMarker + ": " +
			"Actual layout is microservices under services/ " +
			"(auth, billing, notifications, widgets). " +
			"ARCHITECTURE.md is stale and still describes api/core/db monolith.", nil
	case 2:
		return ExploreNotesMarker + ": " +
			"NOTES: verified services/ tree; ARCHITECTURE.md outdated.", nil
	case 3:
		if err := validateDeployHistoryResource(req.Resources); err != nil {
			return "", err
		}
		return ExploreDeployMarker + ": " +
			"DEPLOY_HISTORY confirms monolith -> microservices migration; " +
			"earlier answer stands.", nil
	default:
		return "", fmt.Errorf("unexpected explore turn %d", turnNum)
	}
}

func validateRepoZipResource(resources []json.RawMessage) error {
	data, ok := fileBytesAtMount(resources, ExploreRepoMountPath)
	if !ok {
		return fmt.Errorf(
			"expected file resource at mount_path %q",
			ExploreRepoMountPath,
		)
	}
	if !zipContainsPath(data, "services/auth/main.py") {
		return fmt.Errorf("repo.zip missing services/ microservices layout")
	}
	if !zipContainsPath(data, "ARCHITECTURE.md") {
		return fmt.Errorf("repo.zip missing ARCHITECTURE.md")
	}
	return nil
}

func validateDeployHistoryResource(resources []json.RawMessage) error {
	if err := validateRepoZipResource(resources); err != nil {
		return err
	}
	data, ok := fileBytesAtMount(resources, ExploreDeployHistoryMountPath)
	if !ok {
		return fmt.Errorf(
			"expected file resource at mount_path %q",
			ExploreDeployHistoryMountPath,
		)
	}
	if !strings.Contains(string(data), "microservices") {
		return fmt.Errorf("DEPLOY_HISTORY missing migration hint")
	}
	return nil
}

func fileBytesAtMount(
	resources []json.RawMessage,
	mountPath string,
) ([]byte, bool) {
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "file" {
			continue
		}
		mp, _ := res["mount_path"].(string)
		if mp != mountPath {
			continue
		}
		b64, _ := res["content_base64"].(string)
		if b64 == "" {
			return nil, false
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, false
		}
		return data, true
	}
	return nil, false
}

func zipContainsPath(zipBytes []byte, path string) bool {
	reader, err := zip.NewReader(
		bytes.NewReader(zipBytes),
		int64(len(zipBytes)),
	)
	if err != nil {
		return false
	}
	for _, f := range reader.File {
		if f.Name == path {
			return true
		}
	}
	return false
}

func emitExploreMessage(
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
