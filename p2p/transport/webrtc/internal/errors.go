package internal

import "errors"

var (
	ErrNilParam     = errors.New("nil parameter")
	ErrInvalidParam = errors.New("invalid parameter")
)
