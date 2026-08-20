package harness

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// OpenClawClient calls an OpenClaw Gateway's OpenResponses API
// (POST /v1/responses). Unlike the legacy OpenAI chat-completions
// endpoint, the OpenResponses API emits a rich SSE stream covering the
// full response lifecycle — function_call items surface as agent.tool_use
// and agent.tool_result events, and text deltas stream progressively as
// agent.message events. This lets the session detail UI show tool calls
// and streaming text for OpenClaw turns, same as Hermes.
//
// Conversation state is maintained server-side keyed by the
// x-openclaw-session-key header (oma-{sessionID}). Each RunTurn sends
// only the latest user message — no client-side history replay.
//
// Implements both Client (RunTurn) and StreamingClient (RunTurnStream).
type OpenClawClient struct {
	// GatewayURL is the base URL of the OpenClaw Gateway, e.g.
	// "http://124.221.28.203:17772". No trailing slash.
	GatewayURL string
	// Token is the Bearer token for Gateway auth.
	Token string
	// Agent is the OpenClaw model id, e.g. "openclaw/default".
	// Falls back to "openclaw/default" when empty.
	Agent string
	// HTTP is the optional *http.Client override. When nil a default
	// client with a 10-minute timeout is used.
	HTTP *http.Client
}

// --- OpenResponses API types -----------------------------------------------

// openResponsesRequest is the POST /v1/responses request body. input is
// the latest user message (string form); OpenClaw maintains conversation
// state server-side via x-openclaw-session-key so we don't replay
// history.
type openResponsesRequest struct {
	Model        string `json:"model"`
	Input        string `json:"input"`
	Instructions string `json:"instructions,omitempty"`
	Stream       bool   `json:"stream,omitempty"`
}

// openResponsesEvent is one SSE event from the streaming response.
// Each line is "data: {json}" where the JSON carries a "type" field
// naming the event. Observed event types (per OpenResponses API docs
// and the OpenAI Responses API format):
//
//	response.created          — response object created
//	response.in_progress      — processing started
//	response.output_item.added   — new output item (message/function_call)
//	response.content_part.added  — new content part (output_text)
//	response.output_text.delta   — incremental text chunk
//	response.output_text.done    — text content part complete
//	response.content_part.done   — content part finalized
//	response.output_item.done    — output item finalized
//	response.completed        — terminal state with usage
//	response.failed           — terminal error
type openResponsesEvent struct {
	Type         string             `json:"type"`
	Response     *openResponsesObj  `json:"response,omitempty"`
	OutputIndex  int                `json:"output_index,omitempty"`
	ContentIndex int                `json:"content_index,omitempty"`
	Item         *openResponsesItem `json:"item,omitempty"`
	Part         *openResponsesPart `json:"part,omitempty"`
	Delta        string             `json:"delta,omitempty"`
	Text         string             `json:"text,omitempty"`
	// Error fields — response.failed events may carry error info in
	// the response object (response.error.type/message) or as a
	// top-level error field.
	ErrorType    string `json:"error,omitempty"`
	ErrorMessage string `json:"message,omitempty"`
}

// openResponsesObj is the response object carried by response.created,
// response.in_progress, response.completed, and response.failed events.
type openResponsesObj struct {
	ID     string              `json:"id"`
	Status string              `json:"status"`
	Output []openResponsesItem `json:"output,omitempty"`
	Usage  *openClawUsage      `json:"usage,omitempty"`
	Error  *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// openResponsesItem is one output item. type discriminates:
//
//	"message"        — assistant message (carries content parts)
//	"function_call"  — tool invocation (carries name + arguments)
type openResponsesItem struct {
	Type      string                `json:"type"`
	ID        string                `json:"id,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Role      string                `json:"role,omitempty"`
	Content   []openResponsesPart   `json:"content,omitempty"`
}

// openResponsesPart is one content part inside a message item. Only
// "output_text" is emitted as output by OpenClaw today.
type openResponsesPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// --- Client interface implementations --------------------------------------

// RunTurn implements Client. Runs an OpenResponses turn and collects all
// emitted events into a TurnResponse.
func (c *OpenClawClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	events, usage, err := c.doResponse(ctx, req, nil)
	if err != nil {
		return TurnResponse{}, err
	}
	return TurnResponse{Events: events, Usage: usage}, nil
}

// RunTurnStream implements StreamingClient. Runs an OpenResponses turn
// and emits events progressively as they arrive from the SSE stream.
func (c *OpenClawClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	_, _, err := c.doResponse(ctx, req, onEvent)
	return err
}

// doResponse is the shared RunTurn / RunTurnStream implementation.
//
// It POSTs /v1/responses with stream=true, then parses the SSE stream
// and maps each OpenResponses event to one or more oma session events.
// When onEvent is non-nil events are emitted progressively (streaming);
// otherwise they are accumulated into the returned slice.
//
// Event mapping:
//
//	response.output_item.added{function_call} → (track)
//	response.output_item.done{function_call}  → agent.tool_use + agent.tool_result
//	response.output_text.delta                → agent.message (accumulated text)
//	response.completed                        → final agent.message (if no deltas) + span.model_request_end
//	response.failed                           → error return
func (c *OpenClawClient) doResponse(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) ([]json.RawMessage, *TurnUsage, error) {
	start := time.Now()
	model := c.Agent
	if model == "" {
		model = "openclaw/default"
	}

	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	// --- 1. POST /v1/responses with stream=true. -----------------------
	createBody, err := json.Marshal(openResponsesRequest{
		Model:        model,
		Input:        userText,
		Instructions: req.Agent.SystemPrompt,
		Stream:       true,
	})
	if err != nil {
		return nil, nil, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/v1/responses",
		bytes.NewReader(createBody),
	)
	if err != nil {
		return nil, nil, err
	}
	c.setHeaders(httpReq, req.SessionID)

	// Use streamingHTTPClient (no timeout) because the response body is
	// the SSE stream — a finite timeout would cut off long-running turns.
	client := c.streamingHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		logTurn("backend", "openclaw", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err)
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		logTurn("backend", "openclaw", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", time.Since(start).Milliseconds(),
			"status", resp.StatusCode,
			"error", strings.TrimSpace(string(raw)))
		return nil, nil, fmt.Errorf(
			"openclaw status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	// --- 2. Parse the SSE event stream. --------------------------------
	scanner := bufio.NewScanner(resp.Body)
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
	// functionCallPreview tracks the arguments of the most recently
	// added function_call output item, so we can emit agent.tool_use
	// when response.output_item.done fires.
	var functionCallPreview string

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

		var ev openResponsesEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "response.output_item.added":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				// Capture preview — emit tool_use when
				// output_item.done fires with the full
				// arguments.
				functionCallPreview = ev.Item.Arguments
			}

		case "response.output_item.done":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				name := ev.Item.Name
				preview := ev.Item.Arguments
				if preview == "" {
					preview = functionCallPreview
				}
				// Emit tool_use + tool_result back-to-back.
				// OpenClaw executes tools server-side; we
				// surface the function_call as an observable
				// event pair so the UI shows tool activity.
				if mapped, mapErr := agentToolUseEvent(name, preview); mapErr == nil {
					if err := emit(mapped); err != nil {
						return events, nil, err
					}
				}
				if mapped, mapErr := agentToolResultEvent(name, "(completed)"); mapErr == nil {
					if err := emit(mapped); err != nil {
						return events, nil, err
					}
				}
				functionCallPreview = ""
			}

		case "response.output_text.delta":
			if ev.Delta == "" {
				continue
			}
			accumulated.WriteString(ev.Delta)
			mapped, mapErr := agentMessageEvent(randomOCID(), msgID, accumulated.String())
			if mapErr != nil {
				return events, nil, mapErr
			}
			if err := emit(mapped); err != nil {
				return events, nil, err
			}
			emittedContent = true

		case "response.completed":
			if ev.Response != nil && ev.Response.Usage != nil {
				finalUsage = ev.Response.Usage
			}
			// If no deltas arrived (e.g. tool-only run with a
			// short final answer), emit the assembled output
			// as a single agent.message.
			if !emittedContent && ev.Response != nil {
				text := assembleOutputText(ev.Response.Output)
				if text != "" {
					mapped, mapErr := agentMessageEvent(randomOCID(), msgID, text)
					if mapErr != nil {
						return events, nil, mapErr
					}
					if err := emit(mapped); err != nil {
						return events, nil, err
					}
					emittedContent = true
				}
			}

		case "response.failed":
			terminalErr = true
			msg := ""
			if ev.Response != nil && ev.Response.Error != nil {
				msg = ev.Response.Error.Message
			}
			if msg == "" {
				msg = ev.ErrorMessage
			}
			if msg == "" {
				msg = "response failed"
			}
			return events, nil, fmt.Errorf("openclaw: %s", msg)

		// response.created, response.in_progress,
		// response.content_part.added, response.output_text.done,
		// response.content_part.done — lifecycle events, no oma
		// mapping needed.
		}
	}
	if err := scanner.Err(); err != nil {
		return events, nil, fmt.Errorf("openclaw stream read: %w", err)
	}

	duration := time.Since(start)
	usage := finalUsage.toTurnUsage()

	// Always emit at least one agent.message so downstream consumers
	// never see a turn with only tool events and no text output.
	if !emittedContent {
		mapped, mapErr := agentMessageEvent(randomOCID(), msgID, "")
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
		if usageEv, err := usageEvent(model, "openclaw", duration, usage); err == nil {
			if err := emit(usageEv); err != nil {
				return events, nil, err
			}
		}
		logTurn("backend", "openclaw", "session", req.SessionID,
			"model", model, "stream", onEvent != nil,
			"duration_ms", duration.Milliseconds(),
			"input_tokens", valueOrZero(usage, func(u *TurnUsage) int { return u.InputTokens }),
			"output_tokens", valueOrZero(usage, func(u *TurnUsage) int { return u.OutputTokens }),
			"chars", accumulated.Len())
	}

	return events, usage, nil
}

// assembleOutputText extracts text from a completed response's output
// items. Used as a fallback when no deltas arrived during streaming —
// typically happens for tool-only runs that produce a short final
// answer without progressive text streaming.
func assembleOutputText(items []openResponsesItem) string {
	var b strings.Builder
	for _, item := range items {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" && part.Text != "" {
				b.WriteString(part.Text)
			}
		}
	}
	return b.String()
}

// --- HTTP helpers ----------------------------------------------------------

// setHeaders applies auth, content-type and session routing headers.
func (c *OpenClawClient) setHeaders(req *http.Request, sessionID string) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if sessionID != "" {
		req.Header.Set("x-openclaw-session-key", "oma-"+sessionID)
	}
}

// streamingHTTPClient returns an http.Client without timeout — needed
// because the response body of POST /v1/responses is the SSE stream
// itself, and a finite timeout would cut off long-running turns.
func (c *OpenClawClient) streamingHTTPClient() *http.Client {
	if c.HTTP != nil && c.HTTP.Timeout == 0 {
		return c.HTTP
	}
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	return &http.Client{Transport: base.Transport, Timeout: 0}
}

// --- openClawUsage (shared with hermes_client.go) --------------------------

// openClawUsage is the usage block returned by both OpenClaw and Hermes.
// OpenClaw uses the OpenAI naming (prompt_tokens / completion_tokens);
// Hermes's Runs API uses input_tokens / output_tokens. Both are accepted
// and unified here — see toTurnUsage for the resolution.
type openClawUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// Hermes Runs API naming (input_tokens / output_tokens). When both
	// flavors are present (shouldn't happen), the OpenAI fields take
	// precedence so legacy callers keep working.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// OpenAI-style cache fields — present when the upstream supports
	// prompt caching.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// toTurnUsage converts the upstream usage block to TurnUsage. Nil-safe.
// Resolves OpenAI (prompt_tokens) vs Hermes (input_tokens) naming — if
// the OpenAI field is zero and the Hermes field is non-zero, use the
// Hermes field.
func (u *openClawUsage) toTurnUsage() *TurnUsage {
	if u == nil {
		return nil
	}
	in := u.PromptTokens
	if in == 0 {
		in = u.InputTokens
	}
	out := u.CompletionTokens
	if out == 0 {
		out = u.OutputTokens
	}
	tu := &TurnUsage{
		InputTokens:  in,
		OutputTokens: out,
	}
	if u.PromptTokensDetails != nil {
		tu.CacheReadInputTokens = u.PromptTokensDetails.CachedTokens
	}
	return tu
}

// --- Shared helpers (used by hermes_client.go too) -------------------------

// extractLastUserMessage scans events backwards for the most recent
// user.message and returns its text content. Returns "" when no user
// message is found.
func extractLastUserMessage(events []json.RawMessage) string {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type event struct {
		Type    string         `json:"type"`
		Content []contentBlock `json:"content"`
	}

	for i := len(events) - 1; i >= 0; i-- {
		var ev event
		if err := json.Unmarshal(events[i], &ev); err != nil {
			continue
		}
		if ev.Type != "user.message" {
			continue
		}
		for _, block := range ev.Content {
			if block.Type == "text" && block.Text != "" {
				return block.Text
			}
		}
	}
	return ""
}

// agentMessageEvent builds an oma-format agent.message event.
// id is a unique event ID; messageID is for stream correlation (optional,
// set to "" when not needed).
func agentMessageEvent(id, messageID, text string) (json.RawMessage, error) {
	event := map[string]any{
		"type": "agent.message",
		"id":   id,
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
	if messageID != "" {
		event["message_id"] = messageID
	}
	return json.Marshal(event)
}

const ocIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// randomOCID generates a 12-char random id for agent.message events.
func randomOCID() string {
	out := make([]byte, 12)
	max := big.NewInt(int64(len(ocIDAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand.Read only fails on broken PRNG — degrade
			// gracefully rather than panic.
			out[i] = ocIDAlphabet[i%len(ocIDAlphabet)]
			continue
		}
		out[i] = ocIDAlphabet[idx.Int64()]
	}
	return "oc_" + string(out)
}
