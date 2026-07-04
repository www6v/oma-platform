package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestOutcomeGraderCookbook runs define_outcome + grade-revise loop end-to-end.
func TestOutcomeGraderCookbook(t *testing.T) {
	sim := &demo.OutcomeSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunOutcomeGraderCookbookFlow(t, handler, sim)
}

func TestOutcomeGraderRubricFile(t *testing.T) {
	sim := &demo.OutcomeSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunOutcomeGraderRubricFileFlow(t, handler, sim)
}
