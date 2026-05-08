// Package errs defines sentinel errors shared across application layers.
package errs

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrHiddifyAPI = errors.New("hiddify api error")
)
