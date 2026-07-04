package demo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestOrchestrateSimulatingClientTwoTurns(t *testing.T) {
	t.Parallel()
	sim := &OrchestrateSimulatingClient{}
	workdir := t.TempDir()
	zipBytes := makeOrchestrateTestZip(t)
	env := json.RawMessage(`{
		"config":{
			"type":"cloud",
			"networking":{"type":"limited","allow_package_managers":true},
			"packages":{"pip":["pytest"]}
		}
	}`)

	turn1 := harness.TurnRequest{
		Workdir:     workdir,
		Environment: env,
		Resources: []json.RawMessage{
			mustExploreFileResource(t, OrchestrateRepoMountPath, zipBytes),
		},
	}
	var turn1Text string
	if err := sim.RunTurnStream(context.Background(), turn1, func(ev json.RawMessage) error {
		turn1Text = exploreMessageText(t, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn1Text, OrchestrateTurn1Marker) {
		t.Fatalf("turn1=%q", turn1Text)
	}

	turn2 := harness.TurnRequest{
		Workdir:     workdir,
		Environment: env,
		Resources:   turn1.Resources,
	}
	var turn2Text string
	if err := sim.RunTurnStream(context.Background(), turn2, func(ev json.RawMessage) error {
		turn2Text = exploreMessageText(t, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn2Text, OrchestrateVerifyMarker) {
		t.Fatalf("turn2=%q", turn2Text)
	}
	if !strings.Contains(turn2Text, `"state": "merged"`) {
		t.Fatalf("verify missing merged state: %q", turn2Text)
	}
}

func makeOrchestrateTestZip(t *testing.T) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	entries := map[string]string{
		"gh-mock":              "#!/bin/bash\n",
		"issue_42.json":        `{"number":42}`,
		"src/url_utils.py":     "def slugify(s): ...\n",
		"tests/test_urls.py":   "def test_slugify_unicode(): ...\n",
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
