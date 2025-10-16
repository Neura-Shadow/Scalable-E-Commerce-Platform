package dto

import "goshop/pkg/paging"

type Inventory struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

type ListInventoryReq struct {
	ProductID string `json:"product_id,omitempty" form:"product_id"`
	Page      int64  `json:"-" form:"page"`
	Limit     int64  `json:"-" form:"limit"`
	OrderBy   string `json:"-" form:"order_by"`
	OrderDesc bool   `json:"-" form:"order_desc"`
}

type ListInventoryRes struct {
	Items      []*Inventory       `json:"items"`
	Pagination *paging.Pagination `json:"pagination"`
}

type SetInventoryReq struct {
	ProductID string `json:"product_id" validate:"required"`
	Quantity  int64  `json:"quantity" validate:"gte=0"`
}

type AdjustInventoryReq struct {
	QuantityDelta int64 `json:"quantity_delta" validate:"required"`
}
