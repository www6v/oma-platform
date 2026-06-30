package store

const (
	sessionPrefix  = "sess-"
	resourcePrefix = "sesrsc-"
	eventPrefix    = "evt-"
	schedulePrefix = "sched-"
	teamPrefix     = "team-"
	memberPrefix   = "tmem-"
	tmsgPrefix     = "tmsg-"
	ttaskPrefix    = "ttsk-"
	threadPrefix   = "sthr_"
)

func generateSessionID() string {
	return sessionPrefix + randomString(idLength)
}

func generateResourceID() string {
	return resourcePrefix + randomString(idLength)
}

// NewResourceID returns a new session resource identifier.
func NewResourceID() string {
	return generateResourceID()
}

func generateEventID() string {
	return eventPrefix + randomString(idLength)
}

func generateScheduleID() string {
	return schedulePrefix + randomString(idLength)
}

func generateTeamID() string {
	return teamPrefix + randomString(idLength)
}

func generateTeamMemberID() string {
	return memberPrefix + randomString(idLength)
}

func generateTeamMessageID() string {
	return tmsgPrefix + randomString(idLength)
}

func generateTeamTaskID() string {
	return ttaskPrefix + randomString(idLength)
}

func generateThreadID() string {
	return threadPrefix + randomString(idLength)
}

// NewEventID returns a unique event id for span / lifecycle events.
func NewEventID() string {
	return generateEventID()
}

// NewScheduleID returns a unique wakeup schedule id.
func NewScheduleID() string {
	return generateScheduleID()
}

// NewTeamID returns a unique team id.
func NewTeamID() string {
	return generateTeamID()
}

// NewTeamMemberID returns a unique team member id.
func NewTeamMemberID() string {
	return generateTeamMemberID()
}

// NewTeamMessageID returns a unique team mailbox message id.
func NewTeamMessageID() string {
	return generateTeamMessageID()
}

// NewTeamTaskID returns a unique team task id.
func NewTeamTaskID() string {
	return generateTeamTaskID()
}

// NewThreadID returns a unique session thread id (non-primary).
func NewThreadID() string {
	return generateThreadID()
}
