package domain

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrHiddifyAPI = errors.New("hiddify api error")
)
