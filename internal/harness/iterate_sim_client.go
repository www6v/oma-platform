package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// IterateTurn1Marker is emitted after the first turn (fix loop).
	IterateTurn1Marker = "iterate-cookbook-turn-1-ok"
	// IterateTurn2Marker is emitted after the verify turn.
	IterateTurn2Marker = "iterate-cookbook-turn-2-ok"
	// IterateOutputFilename is written to session outputs on turn 1.
	IterateOutputFilename = "calc.py"
)

// fixedCalcSource is the corrected calc.py written on turn 1.
const fixedCalcSource = `def add(a, b):
    return a + b


def divide(a, b):
    if b == 0:
        raise ValueError("division by zero")
    return a / b


def mean(xs):
    total = 0
    for x in xs:
        total = add(total, x)
    return divide(total, len(xs))
`

// IterateSimulatingClient validates multi-turn session flow for the iterate
// cookbook: turn 1 mounts fixtures and writes fixed calc.py; turn 2 verifies.
type IterateSimulatingClient struct {
	RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements Client.
func (c *IterateSimulatingClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	return TurnResponse{Events: events}, err
}

// RunTurnStream implements StreamingClient.
func (c *IterateSimulatingClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	c.mu.Lock()
	c.turns++
	turnNum := c.turns
	c.mu.Unlock()

	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()

	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if len(req.Resources) == 0 {
		return fmt.Errorf("expected session resources on turn request")
	}

	mounted, err := mountFileResources(req.Workdir, req.Resources)
	if err != nil {
		return err
	}
	if len(mounted) < 2 {
		return fmt.Errorf("expected calc.py and test_calc.py mounts, got %d", len(mounted))
	}

	var stream []map[string]any
	switch turnNum {
	case 1:
		stream, err = c.turn1Stream(mounted, req.Workdir)
	case 2:
		stream, err = c.turn2Stream(req.Workdir)
	default:
		return fmt.Errorf("unexpected turn number %d", turnNum)
	}
	if err != nil {
		return err
	}

	for _, item := range stream {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err := onEvent(raw); err != nil {
			return err
		}
	}
	return nil
}

// TurnCount returns how many harness turns ran.
func (c *IterateSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}

func (c *IterateSimulatingClient) turn1Stream(
	mounted []string,
	workdir string,
) ([]map[string]any, error) {
	calcPath := findMountedBySuffix(mounted, "calc.py")
	testPath := findMountedBySuffix(mounted, "test_calc.py")
	if calcPath == "" || testPath == "" {
		return nil, fmt.Errorf("missing calc.py or test_calc.py in mounts %v", mounted)
	}
	calcData, err := os.ReadFile(calcPath)
	if err != nil {
		return nil, fmt.Errorf("read mounted calc.py: %w", err)
	}
	if !strings.Contains(string(calcData), "BUG") {
		return nil, fmt.Errorf("expected buggy calc.py fixture")
	}

	outPath := filepath.Join(
		workdir, ".mnt", "session", "outputs", IterateOutputFilename,
	)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir outputs: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(fixedCalcSource), 0o644); err != nil {
		return nil, fmt.Errorf("write fixed calc.py: %w", err)
	}

	return []map[string]any{
		{
			"type": "agent.tool_use",
			"name": "bash",
			"input": map[string]string{
				"command": fmt.Sprintf("python3 -c 'import calc'"),
			},
		},
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": fmt.Sprintf(
						"%s mounts=%d output_bytes=%d",
						IterateTurn1Marker,
						len(mounted),
						len(fixedCalcSource),
					),
				},
			},
		},
	}, nil
}

func (c *IterateSimulatingClient) turn2Stream(workdir string) ([]map[string]any, error) {
	outPath := filepath.Join(
		workdir, ".mnt", "session", "outputs", IterateOutputFilename,
	)
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read output calc.py: %w", err)
	}
	if strings.Contains(string(data), "BUG") {
		return nil, fmt.Errorf("output calc.py still contains BUG marker")
	}
	if !strings.Contains(string(data), "division by zero") {
		return nil, fmt.Errorf("fixed calc.py missing zero check")
	}

	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": fmt.Sprintf(
						"%s verified_bytes=%d",
						IterateTurn2Marker,
						len(data),
					),
				},
			},
		},
	}, nil
}

func findMountedBySuffix(paths []string, suffix string) string {
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			return p
		}
	}
	return ""
}
