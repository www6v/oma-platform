package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// RuntimeClient delegates one harness turn to an ACP agent attached through a
// registered local runtime.
type RuntimeClient struct {
	PlatformBase   string
	InternalSecret string
	Binding        AcpBinding
	Dialer         *websocket.Dialer
}

// RunTurn implements Client by collecting the streamed Runtime events.
func (c *RuntimeClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(event json.RawMessage) error {
		events = append(events, append(json.RawMessage(nil), event...))
		return nil
	})
	return TurnResponse{Events: events}, err
}

// RunTurnStream implements StreamingClient over the RuntimeRoom WebSocket
// protocol used by oma bridge daemon.
func (c *RuntimeClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	if c.Binding.RuntimeID == "" || c.Binding.AcpAgentID == "" {
		return fmt.Errorf("acp-proxy binding is incomplete")
	}
	if req.SessionID == "" {
		return fmt.Errorf("acp-proxy turn requires session_id")
	}
	prompt := extractLastUserMessage(req.Events)
	if prompt == "" {
		prompt = "(continue)"
	}

	wsURL, err := c.attachURL()
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("x-internal-secret", c.InternalSecret)
	headers.Set("x-session-id", req.SessionID)
	if req.TenantID != "" {
		headers.Set("x-harness-tenant", req.TenantID)
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf(
				"attach runtime %s: HTTP %d: %w",
				c.Binding.RuntimeID,
				resp.StatusCode,
				err,
			)
		}
		return fmt.Errorf("attach runtime %s: %w", c.Binding.RuntimeID, err)
	}
	defer conn.Close()

	cancelDone := make(chan struct{})
	defer close(cancelDone)
	turnID := runtimeTurnID()
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.WriteJSON(map[string]any{
				"type":       "session.cancel",
				"session_id": req.SessionID,
				"turn_id":    turnID,
			})
			_ = conn.Close()
		case <-cancelDone:
		}
	}()

	var attached struct {
		Type         string `json:"type"`
		DaemonOnline bool   `json:"daemon_online"`
	}
	if err := conn.ReadJSON(&attached); err != nil {
		return runtimeReadError(ctx, "read runtime attachment", err)
	}
	if attached.Type != "attached" {
		return fmt.Errorf(
			"attach runtime %s: expected attached frame, got %q",
			c.Binding.RuntimeID,
			attached.Type,
		)
	}
	if !attached.DaemonOnline {
		return fmt.Errorf("runtime daemon offline")
	}

	if err := conn.WriteJSON(map[string]any{
		"type":       "session.start",
		"session_id": req.SessionID,
		"tenant_id":  req.TenantID,
		"agent_id":   c.Binding.AcpAgentID,
		"turn_id":    turnID,
	}); err != nil {
		return fmt.Errorf("start ACP session: %w", err)
	}
	if err := waitForRuntimeReady(ctx, conn); err != nil {
		return err
	}
	if err := conn.WriteJSON(map[string]any{
		"type":       "session.prompt",
		"session_id": req.SessionID,
		"tenant_id":  req.TenantID,
		"turn_id":    turnID,
		"text":       prompt,
	}); err != nil {
		return fmt.Errorf("prompt ACP session: %w", err)
	}

	acc := newACPEventAccumulator(turnID, onEvent)
	for {
		var frame runtimeFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return runtimeReadError(ctx, "read ACP turn", err)
		}
		switch frame.Type {
		case "session.event":
			if frame.TurnID != "" && frame.TurnID != turnID {
				continue
			}
			if err := acc.consume(frame.Event); err != nil {
				return err
			}
		case "session.complete":
			if frame.TurnID != "" && frame.TurnID != turnID {
				continue
			}
			return acc.finish()
		case "session.error":
			if frame.TurnID != "" && frame.TurnID != turnID {
				continue
			}
			if frame.Message == "" {
				frame.Message = "ACP runtime error"
			}
			return fmt.Errorf("%s", frame.Message)
		}
	}
}

type runtimeFrame struct {
	Type    string          `json:"type"`
	TurnID  string          `json:"turn_id"`
	Message string          `json:"message"`
	Event   json.RawMessage `json:"event"`
}

func waitForRuntimeReady(
	ctx context.Context,
	conn *websocket.Conn,
) error {
	for {
		var frame runtimeFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return runtimeReadError(ctx, "start ACP session", err)
		}
		switch frame.Type {
		case "session.ready":
			return nil
		case "session.error":
			if frame.Message == "" {
				frame.Message = "ACP session failed to start"
			}
			return fmt.Errorf("%s", frame.Message)
		}
	}
}

func (c *RuntimeClient) attachURL() (string, error) {
	base, err := url.Parse(strings.TrimRight(c.PlatformBase, "/"))
	if err != nil {
		return "", fmt.Errorf("parse platform base: %w", err)
	}
	switch base.Scheme {
	case "http":
		base.Scheme = "ws"
	case "https":
		base.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf(
			"unsupported platform URL scheme %q",
			base.Scheme,
		)
	}
	base.Path = strings.TrimRight(base.Path, "/") +
		"/v1/internal/runtimes/" +
		url.PathEscape(c.Binding.RuntimeID) +
		"/attach-harness"
	query := base.Query()
	// RuntimeRoom normally replays its last ready/error frame to late
	// observers. A per-turn client always sends a fresh session.start, so a
	// replayed ready would race the new start after daemon reconnect.
	query.Set("replay", "0")
	base.RawQuery = query.Encode()
	base.Fragment = ""
	return base.String(), nil
}

func runtimeReadError(ctx context.Context, operation string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func runtimeTurnID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "acp-turn"
	}
	return "acp-" + hex.EncodeToString(raw[:])
}

type acpEventAccumulator struct {
	turnID    string
	onEvent   EventHandler
	messageID string
	text      strings.Builder
	thinking  strings.Builder
	tools     map[string]string
}

func newACPEventAccumulator(
	turnID string,
	onEvent EventHandler,
) *acpEventAccumulator {
	return &acpEventAccumulator{
		turnID:  turnID,
		onEvent: onEvent,
		tools:   make(map[string]string),
	}
}

func (a *acpEventAccumulator) consume(raw json.RawMessage) error {
	var notification struct {
		Update struct {
			SessionUpdate string          `json:"sessionUpdate"`
			MessageID     string          `json:"messageId"`
			Content       json.RawMessage `json:"content"`
			ToolCallID    string          `json:"toolCallId"`
			Title         string          `json:"title"`
			Status        string          `json:"status"`
			RawInput      json.RawMessage `json:"rawInput"`
			RawOutput     json.RawMessage `json:"rawOutput"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &notification); err != nil {
		return nil
	}
	update := notification.Update
	switch update.SessionUpdate {
	case "agent_message_chunk":
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(update.Content, &content) == nil &&
			content.Type == "text" {
			if update.MessageID != "" &&
				a.messageID != "" &&
				update.MessageID != a.messageID {
				// ACP session resume may replay chunks from the previous
				// assistant message before emitting this turn's new message.
				a.text.Reset()
			}
			a.text.WriteString(content.Text)
			if update.MessageID != "" {
				a.messageID = update.MessageID
			}
		}
	case "agent_thought_chunk":
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(update.Content, &content) == nil &&
			content.Type == "text" {
			a.thinking.WriteString(content.Text)
		}
	case "tool_call":
		return a.emitToolUse(update.ToolCallID, update.Title, update.RawInput)
	case "tool_call_update":
		if update.Status == "completed" || update.Status == "failed" {
			return a.emitToolResult(update.ToolCallID, update.RawOutput)
		}
	}
	return nil
}

func (a *acpEventAccumulator) finish() error {
	if a.thinking.Len() > 0 {
		event, err := json.Marshal(map[string]any{
			"type": "agent.thinking",
			"text": a.thinking.String(),
		})
		if err != nil {
			return err
		}
		if err := a.emit(event); err != nil {
			return err
		}
	}
	if a.text.Len() == 0 {
		return nil
	}
	messageID := a.messageID
	if messageID == "" {
		messageID = a.turnID
	}
	event, err := json.Marshal(map[string]any{
		"type":       "agent.message",
		"message_id": messageID,
		"content": []map[string]string{
			{"type": "text", "text": a.text.String()},
		},
	})
	if err != nil {
		return err
	}
	return a.emit(event)
}

func (a *acpEventAccumulator) emitToolUse(
	id, title string,
	rawInput json.RawMessage,
) error {
	if id == "" {
		return nil
	}
	if _, exists := a.tools[id]; exists {
		return nil
	}
	a.tools[id] = title
	input := map[string]any{}
	if len(rawInput) > 0 {
		if err := json.Unmarshal(rawInput, &input); err != nil {
			input = map[string]any{"value": string(rawInput)}
		}
	}
	event, err := json.Marshal(map[string]any{
		"type":  "agent.tool_use",
		"id":    id,
		"name":  title,
		"input": input,
	})
	if err != nil {
		return err
	}
	return a.emit(event)
}

func (a *acpEventAccumulator) emitToolResult(
	id string,
	rawOutput json.RawMessage,
) error {
	if id == "" {
		return nil
	}
	content := "(completed)"
	if len(rawOutput) > 0 && string(rawOutput) != "null" {
		var text string
		if json.Unmarshal(rawOutput, &text) == nil {
			content = text
		} else {
			content = string(rawOutput)
		}
	}
	event, err := json.Marshal(map[string]any{
		"type":        "agent.tool_result",
		"tool_use_id": id,
		"content":     content,
	})
	if err != nil {
		return err
	}
	return a.emit(event)
}

func (a *acpEventAccumulator) emit(event json.RawMessage) error {
	if a.onEvent == nil {
		return nil
	}
	return a.onEvent(event)
}
