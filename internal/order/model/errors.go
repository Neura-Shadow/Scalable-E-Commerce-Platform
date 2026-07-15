package model

import "errors"

var (
	ErrPermissionDenied     = errors.New("permission denied")
	ErrInvalidOrderState    = errors.New("invalid order status")
	ErrInvalidOrderQuantity = errors.New("invalid order quantity")
	ErrInvalidOrderAmount   = errors.New("invalid order amount")
	ErrOrderNotFound        = errors.New("order not found")
	ErrIdempotencyConflict  = errors.New("idempotency key conflicts with an existing request")
	ErrIdempotencyDuplicate = errors.New("idempotent order already exists")
)
