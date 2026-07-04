package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestSreCookbookIncidentResponder runs skill + resources + HITL open_pr → approval.
func TestSreCookbookIncidentResponder(t *testing.T) {
	sim := &demo.SreSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunSreIncidentResponderFlow(t, handler, sim)
}
