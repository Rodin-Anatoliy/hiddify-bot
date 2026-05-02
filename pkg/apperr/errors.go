// Package apperr defines application-level sentinel errors.
package apperr

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrHiddifyAPI    = errors.New("hiddify api error")
)
