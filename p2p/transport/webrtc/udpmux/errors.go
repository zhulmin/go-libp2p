package udpmux

import "errors"

var (
	alreadyClosedErr = errors.New("already closed")
)
