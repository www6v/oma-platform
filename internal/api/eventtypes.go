package api

// Client event types accepted by POST /v1/sessions/:id/events (OMA-aligned).
var allowedClientEventTypes = map[string]struct{}{
	"user.message":            {},
	"user.interrupt":          {},
	"user.tool_confirmation":  {},
	"user.custom_tool_result": {},
	"user.define_outcome":     {},
}

// turnTriggerEventTypes cause a harness turn after append.
var turnTriggerEventTypes = map[string]struct{}{
	"user.message":            {},
	"user.custom_tool_result": {},
}

func isAllowedClientEventType(eventType string) bool {
	_, ok := allowedClientEventTypes[eventType]
	return ok
}

func isTurnTriggerEventType(eventType string) bool {
	_, ok := turnTriggerEventTypes[eventType]
	return ok
}

func isInterruptEventType(eventType string) bool {
	return eventType == "user.interrupt"
}
