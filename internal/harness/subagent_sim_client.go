package harness

import (
	"context"
	"encoding/json"
	"fmt"
)

const e2eSubThreadID = "sthr_e2e_worker"

// SubAgentSimulatingClient is a harness test double that validates
// TurnRequest.SubAgents wiring and emits a realistic sub-agent event stream.
type SubAgentSimulatingClient struct {
	RecordingClient
	WorkerReply  string
	PrimaryReply string
}

// RunTurn implements Client.
func (c *SubAgentSimulatingClient) RunTurn(
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
func (c *SubAgentSimulatingClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()

	if err := validateSubAgentTurnRequest(req); err != nil {
		return err
	}

	workerID, workerName := firstSubAgentFromCallable(req)

	workerText := c.workerReply()
	primaryText := c.primaryReply()

	stream := []map[string]any{
		{
			"type":              "session.thread_created",
			"session_thread_id":   e2eSubThreadID,
			"agent_id":            workerID,
			"agent_name":          workerName,
			"parent_thread_id":    "sthr_primary",
		},
		{
			"type":              "agent.message",
			"session_thread_id": e2eSubThreadID,
			"content": []map[string]string{
				{"type": "text", "text": workerText},
			},
		},
		{
			"type":              "session.thread_idle",
			"session_thread_id": e2eSubThreadID,
		},
		{
			"type": "agent.tool_use",
			"name": fmt.Sprintf("call_agent_%s", workerID),
			"input": map[string]string{
				"message": "delegate smoke task",
			},
		},
		{
			"type": "agent.message",
			"content": []map[string]string{
				{"type": "text", "text": primaryText},
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

func (c *SubAgentSimulatingClient) workerReply() string {
	if c.WorkerReply != "" {
		return c.WorkerReply
	}
	return "subagent-worker-ok"
}

func (c *SubAgentSimulatingClient) primaryReply() string {
	if c.PrimaryReply != "" {
		return c.PrimaryReply
	}
	return "subagent-coordinator-ok"
}

func validateSubAgentTurnRequest(req TurnRequest) error {
	if len(req.Agent.CallableAgents) == 0 ||
		string(req.Agent.CallableAgents) == "null" {
		return fmt.Errorf("expected callable_agents on coordinator agent")
	}
	return nil
}

func firstSubAgentFromCallable(req TurnRequest) (id, name string) {
	var refs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(req.Agent.CallableAgents, &refs); err != nil {
		return "agt_worker", "subagent-worker"
	}
	if len(refs) == 0 || refs[0].ID == "" {
		return "agt_worker", "subagent-worker"
	}
	workerID := refs[0].ID
	if snap, ok := req.SubAgents[workerID]; ok && snap.Name != "" {
		return workerID, snap.Name
	}
	return workerID, "subagent-worker"
}
