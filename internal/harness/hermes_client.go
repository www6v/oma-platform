package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HermesClient calls a Hermes Agent via the Runs API
// (POST /v1/runs + GET /v1/runs/{id}/events). The Runs API maintains
// conversation state server-side keyed by session_id and emits a rich
// SSE stream covering tool lifecycle, incremental text, and final
// output — letting us map to the same session event vocabulary pipy
// emits (agent.tool_use / agent.tool_result / agent.message /
// span.model_request_end).
//
// Replaces the previous OpenAI-chat-completions implementation that
// returned a single agent.message per turn and lost all tool activity.
//
// Implements both Client (RunTurn) and StreamingClient (RunTurnStream).
type HermesClient struct {
	// GatewayURL is the base URL of the Hermes API server, e.g.
	// "http://124.221.28.203:8642". No trailing slash.
	GatewayURL string
	// Token is the Bearer token (API_SERVER_KEY) for auth.
	Token string
	// Model is the Hermes model id. Defaults to "hermes-agent" when empty.
	Model string
	// HTTP is the optional *http.Client override. When nil a default
	// client with a 10-minute timeout is used (Hermes agents may do
	// multi-step tool calls that take a while).
	HTTP *http.Client
}

// hermesRunRequest is the POST /v1/runs request body. input is the
// latest user message; the Runs API maintains conversation state
// server-side keyed by session_id so we don't replay history.
type hermesRunRequest struct {
	Input        string `json:"input"`
	SessionID    string `json:"session_id"`
	Instructions string `json:"instructions,omitempty"`
	Model        string `json:"model,omitempty"`
}

// hermesRunStartResponse is the POST /v1/runs response body. Carries
// the run_id used to subscribe to events.
type hermesRunStartResponse struct {
	RunID string `json:"run_id"`
	ID    string `json:"id"` // alias — some Hermes builds use id
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// hermesRunEvent is one SSE event from GET /v1/runs/{id}/events. Each
// line is "data: {json}" where the JSON carries an "event" field that
// names the event type. Observed event types (verified live against
// 124.221.28.203:8642):
//
//	tool.started     — tool invocation begins (tool, preview)
//	tool.completed   — tool invocation ends (tool, duration, error)
//	message.delta    — incremental assistant text (delta)
//	reasoning.available — reasoning text (text) — skipped for now
//	run.completed    — terminal state (output, usage)
//	run.failed       — terminal error (error, reason)
type hermesRunEvent struct {
	Event    string         `json:"event"`
	RunID    string         `json:"run_id"`
	Tool     string         `json:"tool,omitempty"`
	Preview  string         `json:"preview,omitempty"`
	Duration float64        `json:"duration,omitempty"`
	ErrorVal bool           `json:"error,omitempty"`
	Delta    string         `json:"delta,omitempty"`
	Text     string         `json:"text,omitempty"`
	Output   string         `json:"output,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Usage    *openClawUsage `json:"usage,omitempty"`
}

// RunTurn implements Client. Runs a Hermes turn and collects all
// emitted events into a TurnResponse.
func (c *HermesClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	events, usage, err := c.doRun(ctx, req, nil)
	if err != nil {
		return TurnResponse{}, err
	}
	return TurnResponse{Events: events, Usage: usage}, nil
}

// RunTurnStream implements StreamingClient. Runs a Hermes turn and
// emits events progressively as they arrive from the SSE stream.
func (c *HermesClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	_, _, err := c.doRun(ctx, req, onEvent)
	return err
}

// doRun is the shared RunTurn / RunTurnStream implementation.
//
// It POSTs /v1/runs to create a run, then opens the SSE stream at
// GET /v1/runs/{id}/events and maps each Hermes event to one or more
// oma session events. When onEvent is non-nil events are emitted
// progressively (streaming); otherwise they are accumulated into the
// returned slice (non-streaming).
//
// Event mapping:
//
//	tool.started      → agent.tool_use{name, input:{preview}}
//	tool.completed    → agent.tool_result{name, content:"(completed in Xs)" | "(failed)"}
//	message.delta     → agent.message (accumulated text, same id)
//	run.completed     → final agent.message (if no deltas arrived) + span.model_request_end
//	run.failed        → error return
//	reasoning.available → skipped (oma has no reasoning content block yet)
func (c *HermesClient) doRun(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) ([]json.RawMessage, *TurnUsage, error) {
	start := time.Now()
	model := c.Model
	if model == "" {
		model = "hermes-agent"
	}

	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	// --- 1. Create the run. -------------------------------------------
	createBody, err := json.Marshal(hermesRunRequest{
		Input:        userText,
		SessionID:    req.SessionID,
		Instructions: req.Agent.SystemPrompt,
		Model:        model,
	})
	if err != nil {
		return nil, nil, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/v1/runs",
		bytes.NewReader(createBody),
	)
	if err != nil {
		return nil, nil, err
	}
	c.setHeaders(httpReq)

	client := c.httpClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		logTurn("backend", "hermes", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err)
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		logTurn("backend", "hermes", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"status", resp.StatusCode,
			"error", strings.TrimSpace(string(raw)))
		return nil, nil, fmt.Errorf(
			"hermes status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	var startResp hermesRunStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, nil, fmt.Errorf("hermes decode run start: %w", err)
	}
	if startResp.Error != nil {
		return nil, nil, fmt.Errorf(
			"hermes: %s: %s",
			startResp.Error.Type,
			startResp.Error.Message,
		)
	}
	runID := startResp.RunID
	if runID == "" {
		runID = startResp.ID
	}
	if runID == "" {
		return nil, nil, fmt.Errorf("hermes: empty run_id")
	}

	// --- 2. Subscribe to the SSE event stream. ------------------------
	sseReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		c.GatewayURL+"/v1/runs/"+runID+"/events",
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	sseReq.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		sseReq.Header.Set("Authorization", "Bearer "+c.Token)
	}

	streamClient := c.streamingHTTPClient()
	sseResp, err := streamClient.Do(sseReq)
	if err != nil {
		logTurn("backend", "hermes", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err)
		return nil, nil, err
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(sseResp.Body, 4096))
		logTurn("backend", "hermes", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"status", sseResp.StatusCode,
			"error", strings.TrimSpace(string(raw)))
		return nil, nil, fmt.Errorf(
			"hermes events status=%d: %s",
			sseResp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	// --- 3. Map SSE events → oma session events. ----------------------
	scanner := bufio.NewScanner(sseResp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var events []json.RawMessage
	emit := func(ev json.RawMessage) error {
		if onEvent != nil {
			return onEvent(ev)
		}
		events = append(events, ev)
		return nil
	}

	var accumulated strings.Builder
	msgID := randomOCID()
	emittedContent := false
	var finalUsage *openClawUsage
	terminalErr := false

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(line[6:])
		if string(data) == "[DONE]" {
			break
		}

		var ev hermesRunEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}

		switch ev.Event {
		case "tool.started":
			mapped, mapErr := agentToolUseEvent(ev.Tool, ev.Preview)
			if mapErr != nil {
				return events, nil, mapErr
			}
			if err := emit(mapped); err != nil {
				return events, nil, err
			}

		case "tool.completed":
			content := fmt.Sprintf("(completed in %.3fs)", ev.Duration)
			if ev.ErrorVal {
				content = "(failed)"
			}
			mapped, mapErr := agentToolResultEvent(ev.Tool, content)
			if mapErr != nil {
				return events, nil, mapErr
			}
			if err := emit(mapped); err != nil {
				return events, nil, err
			}

		case "message.delta":
			if ev.Delta == "" {
				continue
			}
			accumulated.WriteString(ev.Delta)
			mapped, mapErr := agentMessageEvent(msgID, accumulated.String())
			if mapErr != nil {
				return events, nil, mapErr
			}
			if err := emit(mapped); err != nil {
				return events, nil, err
			}
			emittedContent = true

		case "reasoning.available":
			// oma has no reasoning content block yet — skip.

		case "run.completed":
			if ev.Usage != nil {
				finalUsage = ev.Usage
			}
			// If no deltas arrived (e.g. tool-only run that
			// produced a short final answer without streaming),
			// emit the full output as a single agent.message.
			if !emittedContent && ev.Output != "" {
				mapped, mapErr := agentMessageEvent(msgID, ev.Output)
				if mapErr != nil {
					return events, nil, mapErr
				}
				if err := emit(mapped); err != nil {
					return events, nil, err
				}
				emittedContent = true
			}

		case "run.failed":
			terminalErr = true
			msg := ev.Reason
			if msg == "" {
				msg = ev.Text
			}
			if msg == "" {
				msg = "run failed"
			}
			return events, nil, fmt.Errorf("hermes run failed: %s", msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return events, nil, fmt.Errorf("hermes stream read: %w", err)
	}

	duration := time.Since(start)
	usage := finalUsage.toTurnUsage()

	// Always emit at least one agent.message so downstream consumers
	// never see a turn with only tool events and no text output.
	if !emittedContent {
		mapped, mapErr := agentMessageEvent(msgID, "")
		if mapErr != nil {
			return events, nil, mapErr
		}
		if err := emit(mapped); err != nil {
			return events, nil, err
		}
	}

	// span.model_request_end feeds the usage.AggregateEvents pipeline
	// and ultimately /v1/cost_report.
	if !terminalErr {
		if usageEv, err := usageEvent(model, "hermes", duration, usage); err == nil {
			if err := emit(usageEv); err != nil {
				return events, nil, err
			}
		}
		logTurn("backend", "hermes", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", duration.Milliseconds(),
			"input_tokens", valueOrZero(usage, func(u *TurnUsage) int { return u.InputTokens }),
			"output_tokens", valueOrZero(usage, func(u *TurnUsage) int { return u.OutputTokens }),
			"chars", accumulated.Len())
	}

	return events, usage, nil
}

// setHeaders applies auth and content-type headers to the POST request.
func (c *HermesClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func (c *HermesClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// Hermes agents may do multi-step tool calls (terminal, web search,
	// etc.) — allow up to 10 minutes.
	return &http.Client{Timeout: 10 * time.Minute}
}

// streamingHTTPClient returns an http.Client without timeout for SSE.
func (c *HermesClient) streamingHTTPClient() *http.Client {
	if c.HTTP != nil && c.HTTP.Timeout == 0 {
		return c.HTTP
	}
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	return &http.Client{Transport: base.Transport, Timeout: 0}
}

// agentToolUseEvent builds an agent.tool_use event. The input payload
// carries the tool's preview string — Hermes's tool.started doesn't
// include the full structured input, so the UI shows the preview.
func agentToolUseEvent(name, preview string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"type":  "agent.tool_use",
		"id":    randomOCID(),
		"name":  name,
		"input": map[string]any{"preview": preview},
	})
}

// agentToolResultEvent builds an agent.tool_result event. Content is a
// synthetic marker — Hermes's tool.completed doesn't include raw tool
// output, so we surface "(completed in Xs)" or "(failed)". The actual
// output appears in the next message.delta.
func agentToolResultEvent(name, content string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"type":    "agent.tool_result",
		"id":      randomOCID(),
		"name":    name,
		"content": content,
	})
}
