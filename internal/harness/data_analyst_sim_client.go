package harness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DataAnalystReportMarker is emitted by the sim client agent.message text.
	DataAnalystReportMarker = "data-analyst-cookbook-ok"
	// DataAnalystReportFilename is the session output file written by the sim.
	DataAnalystReportFilename = "report.html"
)

// DataAnalystSimulatingClient validates session resource mounting and writes a
// session output file, emulating cookbook steps 4–6 without an LLM.
type DataAnalystSimulatingClient struct {
	RecordingClient
	ReportMinBytes int
}

// RunTurn implements Client.
func (c *DataAnalystSimulatingClient) RunTurn(
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
func (c *DataAnalystSimulatingClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
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
	if len(mounted) == 0 {
		return fmt.Errorf("no file resources mounted")
	}

	catOut, err := readMountedCSVSnippet(mounted)
	if err != nil {
		return err
	}

	reportBytes, err := writeSimReport(req.Workdir, c.reportMinBytes())
	if err != nil {
		return err
	}

	stream := []map[string]any{
		{
			"type": "agent.tool_use",
			"name": "bash",
			"input": map[string]string{
				"command": fmt.Sprintf(
					"cat %s",
					mounted[0],
				),
			},
		},
		{
			"type": "agent.tool_result",
			"name": "bash",
			"content": catOut,
		},
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": fmt.Sprintf(
						"%s report_bytes=%d",
						DataAnalystReportMarker,
						reportBytes,
					),
				},
			},
		},
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

func (c *DataAnalystSimulatingClient) reportMinBytes() int {
	if c.ReportMinBytes > 0 {
		return c.ReportMinBytes
	}
	return 11 * 1024
}

func mountFileResources(
	workdir string,
	resources []json.RawMessage,
) ([]string, error) {
	var paths []string
	for _, raw := range resources {
		var spec map[string]any
		if err := json.Unmarshal(raw, &spec); err != nil {
			continue
		}
		if spec["type"] != "file" {
			continue
		}
		mountPath, _ := spec["mount_path"].(string)
		if mountPath == "" {
			fileID, _ := spec["file_id"].(string)
			mountPath = "/mnt/session/uploads/" + fileID
		}
		target, err := resourceTargetPath(workdir, mountPath)
		if err != nil {
			return nil, err
		}
		data, err := decodeResourceContent(spec)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir mount parent: %w", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return nil, fmt.Errorf("write mounted file: %w", err)
		}
		paths = append(paths, target)
	}
	return paths, nil
}

func resourceTargetPath(workdir, mountPath string) (string, error) {
	cleaned := strings.TrimPrefix(strings.TrimSpace(mountPath), "/")
	if cleaned == "" || strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid mount_path %q", mountPath)
	}
	return filepath.Join(workdir, filepath.FromSlash(cleaned)), nil
}

func decodeResourceContent(spec map[string]any) ([]byte, error) {
	rawB64, _ := spec["content_base64"].(string)
	if rawB64 != "" {
		data, err := base64.StdEncoding.DecodeString(rawB64)
		if err != nil {
			return nil, fmt.Errorf("decode resource content: %w", err)
		}
		return data, nil
	}
	content, _ := spec["content"].(string)
	return []byte(content), nil
}

func readMountedCSVSnippet(paths []string) (string, error) {
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return "", fmt.Errorf("read mounted resource: %w", err)
	}
	text := string(data)
	if !strings.Contains(text, "order_id") {
		return "", fmt.Errorf(
			"mounted file missing CSV header, got %q",
			truncateForError(text, 80),
		)
	}
	return truncateForError(text, 120), nil
}

func writeSimReport(workdir string, minBytes int) (int, error) {
	reportPath := filepath.Join(
		workdir, ".mnt", "session", "outputs", DataAnalystReportFilename,
	)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir outputs: %w", err)
	}
	body := strings.Repeat("x", minBytes)
	html := "<!DOCTYPE html><html><body><h1>Report</h1><pre>" +
		body + "</pre></body></html>"
	if err := os.WriteFile(reportPath, []byte(html), 0o644); err != nil {
		return 0, fmt.Errorf("write report: %w", err)
	}
	return len(html), nil
}

func truncateForError(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
