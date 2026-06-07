package store

const (
	sessionPrefix = "sess-"
	eventPrefix   = "evt-"
)

func generateSessionID() string {
	return sessionPrefix + randomString(idLength)
}

func generateEventID() string {
	return eventPrefix + randomString(idLength)
}
