package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// DeepSeekClient calls the DeepSeek harness (dsh) web gateway: RPC at
// POST /api/<method> with ClientRequest/ServerResponse envelopes and a
// WebSocket event feed at /api/events.mux. All dsh protocol knowledge
// is confined to this file.
type DeepSeekClient struct {
	// GatewayURL is the dsh web base URL, e.g. "http://dsh:3080".
	GatewayURL string
	// Token is an optional bearer token (dsh ships without auth).
	Token string
	// HTTP overrides the transport; nil uses a 10-minute timeout client.
	HTTP *http.Client
}

// dshClientRequest is the uplink wire envelope (apiproxy rpc.ts
// ClientRequest): method is dotted, path == method.
type dshClientRequest struct {
	Type    string `json:"type"`
	RpcID   string `json:"rpcId"`
	Method  string `json:"method"`
	Payload any    `json:"payload"`
}

// dshServerResponse is the response envelope. Business errors arrive
// as HTTP 200 with result.ok=false; HTTP status codes are carrier-only.
type dshServerResponse struct {
	Type   string `json:"type"`
	RpcID  string `json:"rpcId"`
	Result struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"result"`
}

// rpc posts one dotted-method call to POST /api/<method>.
func (c *DeepSeekClient) rpc(
	ctx context.Context,
	method string,
	payload map[string]any,
	out any,
) error {
	body, err := json.Marshal(dshClientRequest{
		Type:    "client-request",
		RpcID:   randomOCID(),
		Method:  method,
		Payload: payload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/api/"+method,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("deepseek rpc %s status=%d: %s",
			method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var env dshServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("deepseek rpc %s decode: %w", method, err)
	}
	if !env.Result.OK {
		msg := "unknown error"
		if env.Result.Error != nil {
			msg = env.Result.Error.Code + ": " + env.Result.Error.Message
		}
		return fmt.Errorf("deepseek rpc %s: %s", method, msg)
	}
	if out != nil {
		return json.Unmarshal(env.Result.Value, out)
	}
	return nil
}

func (c *DeepSeekClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// ensureSession creates the dsh session on first use. session-conflict
// means it already exists — not an error.
func (c *DeepSeekClient) ensureSession(
	ctx context.Context, sessionID string,
) error {
	err := c.rpc(ctx, "session.create", map[string]any{
		"sessionId": sessionID,
	}, nil)
	if err != nil && strings.Contains(err.Error(), "session-conflict") {
		return nil
	}
	return err
}

// RunTurn implements Client. It creates the dsh session, prompts, and
// collects events via WebSocket until turn/end.
func (c *DeepSeekClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	start := time.Now()
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	if err := c.ensureSession(ctx, req.SessionID); err != nil {
		logTurn("backend", "deepseek", "session", req.SessionID,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		return TurnResponse{}, err
	}

	if err := c.rpc(ctx, "session.prompt", map[string]any{
		"sessionId": req.SessionID,
		"mode":      "queue",
		"content":   []map[string]any{{"type": "text", "text": userText}},
	}, nil); err != nil {
		logTurn("backend", "deepseek", "session", req.SessionID,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		return TurnResponse{}, err
	}

	events, usage, err := c.collectTurn(ctx, req.SessionID)
	if err != nil {
		logTurn("backend", "deepseek", "session", req.SessionID,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		return TurnResponse{}, err
	}
	logTurn("backend", "deepseek", "session", req.SessionID,
		"duration_ms", time.Since(start).Milliseconds())
	return TurnResponse{Events: events, Usage: usage}, nil
}

// dshServerRequest is one /api/events.mux text message.
type dshServerRequest struct {
	Type    string          `json:"type"`
	RpcID   string          `json:"rpcId"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

// dshMuxFrame is the payload slot; mux aggregates ALL sessions, so the
// client filters on SessionID.
type dshMuxFrame struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Event     dshSessionEvent `json:"event"`
}

// dshSessionEvent is the dsh SessionEvent envelope
// (packages/core/session/src/types.ts): { type, seq, time, data }.
type dshSessionEvent struct {
	Type string          `json:"type"`
	Seq  int             `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// collectTurn dials the WS mux, collects events for this session until
// turn/end, and returns mapped oma events plus token usage.
func (c *DeepSeekClient) collectTurn(
	ctx context.Context,
	sessionID string,
) ([]json.RawMessage, *TurnUsage, error) {
	wsURL := strings.Replace(c.GatewayURL, "http", "ws", 1) +
		"/api/events.mux"
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, nil, fmt.Errorf("deepseek events dial: %w", err)
	}
	defer conn.Close()

	msgID := randomOCID()
	var events []json.RawMessage
	var usage *TurnUsage
	var accumulated strings.Builder
	emitted := false

	for {
		type readResult struct {
			req dshServerRequest
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			var sr dshServerRequest
			err := conn.ReadJSON(&sr)
			ch <- readResult{sr, err}
		}()

		select {
		case <-ctx.Done():
			_ = conn.Close()
			return events, usage, ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return events, usage, fmt.Errorf("ws read: %w", r.err)
			}
			sr := r.req
			if sr.Type != "server-request" {
				continue
			}
			var frame dshMuxFrame
			if err := json.Unmarshal(sr.Payload, &frame); err != nil {
				continue
			}
			if frame.SessionID != sessionID {
				continue
			}
			if frame.Type == "stream/error" {
				return events, usage, fmt.Errorf("stream error from dsh")
			}
			if frame.Type != "session/event" {
				continue
			}

			ev := frame.Event
			switch ev.Type {
			case "assistant/chunk":
				var d struct {
					Chunk struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"chunk"`
				}
				if json.Unmarshal(ev.Data, &d) != nil ||
					d.Chunk.Type != "text-delta" || d.Chunk.Text == "" {
					continue
				}
				accumulated.WriteString(d.Chunk.Text)
				mapped, mapErr := agentMessageEvent(randomOCID(), msgID, accumulated.String())
				if mapErr != nil {
					return events, usage, mapErr
				}
				events = append(events, mapped)
				emitted = true

			case "tool/call":
				var d struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				mapped, mapErr := agentToolUseEvent(d.Name, "")
				if mapErr != nil {
					return events, usage, mapErr
				}
				events = append(events, mapped)

			case "tool/result":
				var d struct {
					Error *struct {
						Name string `json:"name"`
					} `json:"error"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				content := "(completed)"
				if d.Error != nil {
					content = "(failed: " + d.Error.Name + ")"
				}
				mapped, mapErr := agentToolResultEvent("", content)
				if mapErr != nil {
					return events, usage, mapErr
				}
				events = append(events, mapped)

			case "assistant/message":
				var d struct {
					Message struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
					Usage *struct {
						InputTokens  int `json:"inputTokens"`
						OutputTokens int `json:"outputTokens"`
					} `json:"usage"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				var sb strings.Builder
				for _, part := range d.Message.Content {
					if part.Type == "text" {
						sb.WriteString(part.Text)
					}
				}
				if sb.Len() > 0 {
					mapped, mapErr := agentMessageEvent(randomOCID(), msgID, sb.String())
					if mapErr != nil {
						return events, usage, mapErr
					}
					events = append(events, mapped)
					emitted = true
				}
				if d.Usage != nil {
					usage = &TurnUsage{
						InputTokens:  d.Usage.InputTokens,
						OutputTokens: d.Usage.OutputTokens,
					}
				}

			case "turn/end":
				var d struct {
					Reason struct {
						Kind string `json:"kind"`
					} `json:"reason"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				if !emitted {
					mapped, mapErr := agentMessageEvent(randomOCID(), msgID, accumulated.String())
					if mapErr != nil {
						return events, usage, mapErr
					}
					events = append(events, mapped)
				}
				if d.Reason.Kind == "error" {
					return events, usage, fmt.Errorf("deepseek turn ended with error")
				}
				return events, usage, nil
			}
		}
	}
}

// RunTurnStream implements StreamingClient. It prompts the dsh session
// over RPC, then consumes /api/events.mux until turn/end, mapping each
// SessionEvent onto the oma vocabulary as it arrives.
func (c *DeepSeekClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	start := time.Now()
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	if err := c.ensureSession(ctx, req.SessionID); err != nil {
		return err
	}

	if err := c.rpc(ctx, "session.prompt", map[string]any{
		"sessionId": req.SessionID,
		"mode":      "queue",
		"content":   []map[string]any{{"type": "text", "text": userText}},
	}, nil); err != nil {
		return err
	}

	wsURL := strings.Replace(c.GatewayURL, "http", "ws", 1) +
		"/api/events.mux"
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("deepseek events dial: %w", err)
	}
	defer conn.Close()

	msgID := randomOCID()
	var accumulated strings.Builder
	emitted := false

	for {
		type readResult struct {
			req dshServerRequest
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			var sr dshServerRequest
			err := conn.ReadJSON(&sr)
			ch <- readResult{sr, err}
		}()

		select {
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return fmt.Errorf("ws read: %w", r.err)
			}
			sr := r.req
			if sr.Type != "server-request" {
				continue
			}
			var frame dshMuxFrame
			if err := json.Unmarshal(sr.Payload, &frame); err != nil {
				continue
			}
			if frame.SessionID != req.SessionID {
				continue
			}
			if frame.Type == "stream/error" {
				return fmt.Errorf("stream error from dsh")
			}
			if frame.Type != "session/event" {
				continue
			}

			ev := frame.Event
			switch ev.Type {
			case "assistant/chunk":
				var d struct {
					Chunk struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"chunk"`
				}
				if json.Unmarshal(ev.Data, &d) != nil ||
					d.Chunk.Type != "text-delta" || d.Chunk.Text == "" {
					continue
				}
				accumulated.WriteString(d.Chunk.Text)
				mapped, mapErr := agentMessageEvent(randomOCID(), msgID, accumulated.String())
				if mapErr != nil {
					return mapErr
				}
				if err := onEvent(mapped); err != nil {
					return err
				}
				emitted = true

			case "tool/call":
				var d struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				mapped, mapErr := agentToolUseEvent(d.Name, "")
				if mapErr != nil {
					return mapErr
				}
				if err := onEvent(mapped); err != nil {
					return err
				}

			case "tool/result":
				var d struct {
					Error *struct {
						Name string `json:"name"`
					} `json:"error"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				content := "(completed)"
				if d.Error != nil {
					content = "(failed: " + d.Error.Name + ")"
				}
				mapped, mapErr := agentToolResultEvent("", content)
				if mapErr != nil {
					return mapErr
				}
				if err := onEvent(mapped); err != nil {
					return err
				}

			case "assistant/message":
				var d struct {
					Message struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
					Usage *struct {
						InputTokens  int `json:"inputTokens"`
						OutputTokens int `json:"outputTokens"`
					} `json:"usage"`
				}
				if json.Unmarshal(ev.Data, &d) != nil {
					continue
				}
				var sb strings.Builder
				for _, part := range d.Message.Content {
					if part.Type == "text" {
						sb.WriteString(part.Text)
					}
				}
				if sb.Len() > 0 {
					mapped, mapErr := agentMessageEvent(randomOCID(), msgID, sb.String())
					if mapErr != nil {
						return mapErr
					}
					if err := onEvent(mapped); err != nil {
						return err
					}
					emitted = true
				}

			case "turn/end":
				var d struct {
					Reason struct {
						Kind string `json:"kind"`
					} `json:"reason"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				if !emitted {
					mapped, mapErr := agentMessageEvent(randomOCID(), msgID, accumulated.String())
					if mapErr != nil {
						return mapErr
					}
					if err := onEvent(mapped); err != nil {
						return err
					}
				}
				logTurn("backend", "deepseek", "session", req.SessionID,
					"stream", true,
					"duration_ms", time.Since(start).Milliseconds(),
					"chars", accumulated.Len())
				if d.Reason.Kind == "error" {
					return fmt.Errorf("deepseek turn ended with error")
				}
				return nil
			}
		}
	}
}
