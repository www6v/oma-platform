package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestOrchestrateCookbookIssueToPR runs multi-turn issue→PR chain with mock gh zip.
func TestOrchestrateCookbookIssueToPR(t *testing.T) {
	sim := &demo.OrchestrateSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunOrchestrateIssueToPRFlow(t, handler, sim)
}
