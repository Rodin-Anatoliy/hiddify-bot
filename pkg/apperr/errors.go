package apperr

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrHiddifyAPI    = errors.New("hiddify api error")
	ErrUnauthorized  = errors.New("unauthorized")
)

func Is(err, target error) bool { return errors.Is(err, target) }
