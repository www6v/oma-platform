package integration_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

func TestOutcomeGraderCookbook(t *testing.T) {
	sim := &demo.OutcomeSimulatingClient{}
	handler := newGateCookbookRouter(t, sim)
	integrationtest.RunOutcomeGraderCookbookFlow(t, handler, sim)
}

func TestOutcomeGraderRubricFile(t *testing.T) {
	sim := &demo.OutcomeSimulatingClient{}
	handler := newGateCookbookRouter(t, sim)
	integrationtest.RunOutcomeGraderRubricFileFlow(t, handler, sim)
}
