package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// CoordinateCompleteMarker ends the cookbook stream on the primary thread.
	CoordinateCompleteMarker = "coordinate-cookbook-complete-ok"
	// Specialist names from CMA_coordinate_specialist_team.ipynb.
	SpecialistResearcher = "prospect_researcher"
	SpecialistLibrarian  = "case_study_picker"
	SpecialistPricer     = "pricing_modeler"
)

var coordinateSpecialistOrder = []string{
	SpecialistResearcher,
	SpecialistPricer,
	SpecialistLibrarian,
}

// CoordinateSimulatingClient validates multiagent coordinator wiring and emits
// thread_created + thread_message_received for three specialists.
type CoordinateSimulatingClient struct {
	harness.RecordingClient
}

// RunTurn implements harness.Client.
func (c *CoordinateSimulatingClient) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	return harness.TurnResponse{Events: events}, err
}

// RunTurnStream implements harness.StreamingClient.
func (c *CoordinateSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	if err := validateCoordinateTurnRequest(req); err != nil {
		return err
	}
	c.RecordRequest(req)

	byName := indexSubAgentsByName(req.SubAgents)
	stream := buildCoordinateStream(byName)
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

func validateCoordinateTurnRequest(req harness.TurnRequest) error {
	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if len(req.Resources) == 0 {
		return fmt.Errorf("expected session resources on turn request")
	}
	if len(req.SubAgents) < 3 {
		return fmt.Errorf(
			"expected 3 sub-agents, got %d",
			len(req.SubAgents),
		)
	}
	return nil
}

func indexSubAgentsByName(
	subAgents map[string]harness.AgentSnapshot,
) map[string]harness.AgentSnapshot {
	out := make(map[string]harness.AgentSnapshot, len(subAgents))
	for id, snap := range subAgents {
		name := snap.Name
		if name == "" {
			name = id
		}
		out[name] = snap
		out[id] = snap
	}
	return out
}

func buildCoordinateStream(
	byName map[string]harness.AgentSnapshot,
) []map[string]any {
	stream := make([]map[string]any, 0, 10)
	threadByName := map[string]string{
		SpecialistResearcher: "sthr_coord_researcher",
		SpecialistPricer:     "sthr_coord_pricer",
		SpecialistLibrarian:  "sthr_coord_librarian",
	}
	reportByName := map[string]map[string]any{
		SpecialistResearcher: {
			"priorities":   []string{"operational efficiency", "patient throughput"},
			"recent_moves": []string{"EHR consolidation"},
			"pain_points":  []string{"manual scheduling"},
			"sources":      []string{"industry-brief-2026"},
		},
		SpecialistLibrarian: {
			"picks": []map[string]string{
				{
					"file":         "regional_health.md",
					"customer":     "Regional Health Co",
					"why_relevant": "Healthcare system at similar scale",
				},
				{
					"file":         "metro_clinic.md",
					"customer":     "Metro Clinic Network",
					"why_relevant": "Heavy outpatient workflow",
				},
			},
		},
		SpecialistPricer: {
			"options": []map[string]any{
				{
					"name":           "enterprise",
					"structure":      "annual commit + platform fee",
					"year_one_total": 420000,
				},
				{
					"name":           "flexible",
					"structure":      "monthly per seat",
					"year_one_total": 468000,
				},
			},
		},
	}

	for _, name := range coordinateSpecialistOrder {
		snap, ok := byName[name]
		if !ok {
			continue
		}
		threadID := threadByName[name]
		stream = append(stream, map[string]any{
			"type":              "session.thread_created",
			"session_thread_id": threadID,
			"agent_id":          snap.ID,
			"agent_name":        name,
			"parent_thread_id":  "sthr_primary",
		})
	}

	for _, name := range coordinateSpecialistOrder {
		snap, ok := byName[name]
		if !ok {
			continue
		}
		threadID := threadByName[name]
		payload, _ := json.Marshal(reportByName[name])
		stream = append(stream,
			map[string]any{
				"type":              "agent.message",
				"session_thread_id": threadID,
				"content": []map[string]string{
					{
						"type": "text",
						"text": string(payload),
					},
				},
			},
			map[string]any{
				"type":              "session.thread_idle",
				"session_thread_id": threadID,
			},
			map[string]any{
				"type":           "agent.thread_message_received",
				"from_agent_id":  snap.ID,
				"from_agent_name": name,
				"session_thread_id": "sthr_primary",
				"content": []map[string]string{
					{
						"type": "text",
						"text": string(payload),
					},
				},
			},
		)
	}

	stream = append(stream, map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{
				"type": "text",
				"text": CoordinateCompleteMarker +
					" proposal written to /mnt/session/outputs/proposal.md",
			},
		},
	})
	return stream
}

// SubAgentNames returns sorted specialist names seen on the last request.
func (c *CoordinateSimulatingClient) SubAgentNames() []string {
	last, ok := c.LastRequest()
	if !ok {
		return nil
	}
	names := make([]string, 0, len(last.SubAgents))
	seen := make(map[string]struct{})
	for _, snap := range last.SubAgents {
		if snap.Name == "" || snap.Name == snap.ID {
			continue
		}
		if _, ok := seen[snap.Name]; ok {
			continue
		}
		seen[snap.Name] = struct{}{}
		names = append(names, snap.Name)
	}
	sort.Strings(names)
	return names
}
