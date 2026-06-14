package store

const (
	sessionPrefix  = "sess-"
	eventPrefix    = "evt-"
	schedulePrefix = "sched-"
)

func generateSessionID() string {
	return sessionPrefix + randomString(idLength)
}

func generateEventID() string {
	return eventPrefix + randomString(idLength)
}

func generateScheduleID() string {
	return schedulePrefix + randomString(idLength)
}

// NewEventID returns a unique event id for span / lifecycle events.
func NewEventID() string {
	return generateEventID()
}

// NewScheduleID returns a unique wakeup schedule id.
func NewScheduleID() string {
	return generateScheduleID()
}
