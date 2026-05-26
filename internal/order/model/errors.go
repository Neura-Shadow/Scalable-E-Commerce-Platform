package model

import "errors"

var (
	ErrPermissionDenied  = errors.New("permission denied")
	ErrInvalidOrderState = errors.New("invalid order status")
)
