package demo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestExploreSimulatingClientThreeTurns(t *testing.T) {
	t.Parallel()
	sim := &ExploreSimulatingClient{}
	workdir := t.TempDir()
	zipBytes := makeTestRepoZip(t)

	turn1 := harness.TurnRequest{
		Workdir: workdir,
		Resources: []json.RawMessage{
			mustExploreFileResource(t, ExploreRepoMountPath, zipBytes),
		},
	}
	var turn1Text string
	if err := sim.RunTurnStream(context.Background(), turn1, func(ev json.RawMessage) error {
		turn1Text = exploreMessageText(t, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn1Text, ExploreArchitectureMarker) {
		t.Fatalf("turn1=%q", turn1Text)
	}

	turn2 := harness.TurnRequest{
		Workdir: workdir,
		Resources: turn1.Resources,
	}
	var turn2Text string
	if err := sim.RunTurnStream(context.Background(), turn2, func(ev json.RawMessage) error {
		turn2Text = exploreMessageText(t, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn2Text, ExploreNotesMarker) {
		t.Fatalf("turn2=%q", turn2Text)
	}

	deployBytes := []byte(
		"# DEPLOY HISTORY\n2026-03-01: monolith -> microservices migration complete\n",
	)
	turn3 := harness.TurnRequest{
		Workdir: workdir,
		Resources: []json.RawMessage{
			mustExploreFileResource(t, ExploreRepoMountPath, zipBytes),
			mustExploreFileResource(t, ExploreDeployHistoryMountPath, deployBytes),
		},
	}
	var turn3Text string
	if err := sim.RunTurnStream(context.Background(), turn3, func(ev json.RawMessage) error {
		turn3Text = exploreMessageText(t, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn3Text, ExploreDeployMarker) {
		t.Fatalf("turn3=%q", turn3Text)
	}
}

func makeTestRepoZip(t *testing.T) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	entries := map[string]string{
		"ARCHITECTURE.md":          "# stale monolith doc\n",
		"services/auth/main.py":    "# auth\n",
		"services/billing/main.py": "# billing\n",
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustExploreFileResource(
	t *testing.T,
	mountPath string,
	data []byte,
) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type":           "file",
		"mount_path":     mountPath,
		"content_base64": base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func exploreMessageText(t *testing.T, ev json.RawMessage) string {
	t.Helper()
	var msg struct {
		Type    string `json:"type"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(ev, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "agent.message" || len(msg.Content) == 0 {
		return ""
	}
	return msg.Content[0].Text
}
