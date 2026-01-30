package model

import "errors"

var (
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists is returned when a resource already exists.
	ErrAlreadyExists = errors.New("already exists")
	// ErrNotValid is returned when a resource is not valid.
	ErrNotValid = errors.New("not valid")
)
