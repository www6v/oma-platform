package store

import "errors"

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrArchived is returned when mutating an archived agent.
	ErrArchived = errors.New("agent archived")
)
