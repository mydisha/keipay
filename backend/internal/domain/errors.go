package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned on unique-constraint or duplicate violations.
	ErrConflict = errors.New("conflict")
	// ErrInvalidInput is returned when input validation fails.
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnauthorized is returned when authentication or signature checks fail.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrInactiveTenant is returned when a tenant exists but is disabled.
	ErrInactiveTenant = errors.New("tenant inactive")
)
