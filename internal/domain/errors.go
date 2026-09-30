package domain

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrHiddifyAPI = errors.New("hiddify api error")
)

// StatusError keeps the HTTP status of a failed panel call. It is wrapped
// together with ErrHiddifyAPI, so errors.Is(err, ErrHiddifyAPI) still works.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("status %d", e.Code) }

// IsClearRejection reports whether err is a definite 4xx answer from the panel,
// i.e. the request was understood and refused, so nothing was created.
// Timeouts, network errors and 5xx are NOT clear: the panel may have done the
// work anyway. 404 (wrong path), 408 and 429 are treated as unclear too.
func IsClearRejection(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return se.Code >= 400 && se.Code < 500
}
