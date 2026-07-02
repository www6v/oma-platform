package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestGateCookbookRequiresAction replicates gate HITL GT2/GT3 on the Go server:
// custom_tool_use without tool_result → requires_action idle with event_ids.
func TestGateCookbookRequiresAction(t *testing.T) {
	sim := &harness.GateSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunGateCookbookHitlFlow(t, handler, sim)
}

// TestGateCookbookHitlResume covers Phase D: custom_tool_result promote,
// synthesized agent.tool_result, resume turns, and final end_turn.
func TestGateCookbookHitlResume(t *testing.T) {
	sim := &harness.GateSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunGateCookbookHitlResumeFlow(t, handler, sim)
}
