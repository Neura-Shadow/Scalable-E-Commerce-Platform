package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"

	"goshop/internal/inventory/dto"
	"goshop/internal/inventory/model"
	"goshop/pkg/paging"
)

type IInventoryRepository struct {
	mock.Mock
}

func (_m *IInventoryRepository) List(ctx context.Context, req *dto.ListInventoryReq) ([]*model.Inventory, *paging.Pagination, error) {
	ret := _m.Called(ctx, req)

	var r0 []*model.Inventory
	if rf, ok := ret.Get(0).(func(context.Context, *dto.ListInventoryReq) []*model.Inventory); ok {
		r0 = rf(ctx, req)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*model.Inventory)
		}
	}

	var r1 *paging.Pagination
	if rf, ok := ret.Get(1).(func(context.Context, *dto.ListInventoryReq) *paging.Pagination); ok {
		r1 = rf(ctx, req)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*paging.Pagination)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(context.Context, *dto.ListInventoryReq) error); ok {
		r2 = rf(ctx, req)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

func (_m *IInventoryRepository) GetByProductID(ctx context.Context, productID string) (*model.Inventory, error) {
	ret := _m.Called(ctx, productID)

	var r0 *model.Inventory
	if rf, ok := ret.Get(0).(func(context.Context, string) *model.Inventory); ok {
		r0 = rf(ctx, productID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*model.Inventory)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, string) error); ok {
		r1 = rf(ctx, productID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *IInventoryRepository) Create(ctx context.Context, inventory *model.Inventory) error {
	ret := _m.Called(ctx, inventory)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *model.Inventory) error); ok {
		r0 = rf(ctx, inventory)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

func (_m *IInventoryRepository) Update(ctx context.Context, inventory *model.Inventory) error {
	ret := _m.Called(ctx, inventory)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *model.Inventory) error); ok {
		r0 = rf(ctx, inventory)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

func (_m *IInventoryRepository) ConsumeStock(ctx context.Context, productID string, quantity int64) (bool, error) {
	ret := _m.Called(ctx, productID, quantity)

	var r0 bool
	if rf, ok := ret.Get(0).(func(context.Context, string, int64) bool); ok {
		r0 = rf(ctx, productID, quantity)
	} else {
		r0 = ret.Bool(0)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, string, int64) error); ok {
		r1 = rf(ctx, productID, quantity)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *IInventoryRepository) Restock(ctx context.Context, productID string, quantity int64) error {
	ret := _m.Called(ctx, productID, quantity)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, string, int64) error); ok {
		r0 = rf(ctx, productID, quantity)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
