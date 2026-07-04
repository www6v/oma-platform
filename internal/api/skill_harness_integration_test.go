package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestSkillHarnessInjection verifies agent.skills resolve into harness turn payloads.
func TestSkillHarnessInjection(t *testing.T) {
	sim := &demo.SkillSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunSkillHarnessInjectionFlow(t, handler, sim)
}
