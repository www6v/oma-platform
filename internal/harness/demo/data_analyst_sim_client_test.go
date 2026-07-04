package demo_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/harness/demo"
)

func TestDataAnalystSimulatingClientMountsAndWritesReport(t *testing.T) {
	workdir := t.TempDir()
	csv := []byte("order_id,customer_id\n1001,C001\n")
	resources := []json.RawMessage{
		mustJSON(map[string]any{
			"type":           "file",
			"file_id":        "file_test",
			"mount_path":     "/mnt/session/uploads/sales_data.csv",
			"content_base64": base64.StdEncoding.EncodeToString(csv),
		}),
	}

	client := &demo.DataAnalystSimulatingClient{ReportMinBytes: 2048}
	var types []string
	err := client.RunTurnStream(
		context.Background(),
		harness.TurnRequest{
			SessionID: "sess_test",
			Workdir:   workdir,
			Resources: resources,
		},
		func(ev json.RawMessage) error {
			var meta struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(ev, &meta)
			types = append(types, meta.Type)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) < 3 {
		t.Fatalf("events=%v", types)
	}

	mounted := filepath.Join(
		workdir, "mnt", "session", "uploads", "sales_data.csv",
	)
	data, err := os.ReadFile(mounted)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(csv) {
		t.Fatalf("mounted=%q", string(data))
	}

	report := filepath.Join(
		workdir, ".mnt", "session", "outputs", "report.html",
	)
	info, err := os.Stat(report)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 2048 {
		t.Fatalf("report size=%d", info.Size())
	}
}
