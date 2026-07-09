package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestOperateCookbookInProduction runs vault MCP + session vault_ids probe.
func TestOperateCookbookInProduction(t *testing.T) {
	sim := &demo.OperateSimulatingClient{}
	handler, _ := testOperateRouterHarness(t, sim)
	integrationtest.RunOperateInProductionFlow(t, handler, sim)
}
