package store

import "errors"

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrArchived is returned when mutating an archived agent.
	ErrArchived = errors.New("agent archived")
	// ErrDuplicate is returned on unique constraint violations.
	ErrDuplicate = errors.New("duplicate")
)
