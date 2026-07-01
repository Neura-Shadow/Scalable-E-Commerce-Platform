package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"goshop/internal/inventory/dto"
	productModel "goshop/internal/product/model"
	userModel "goshop/internal/user/model"
)

func TestInventoryAPI_SetStockForbiddenForCustomer(t *testing.T) {
	u := userModel.User{
		Email:    "customer-inventory-set@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "test-product-inventory-set",
		Description: "test-product-inventory-set",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.SetInventoryReq{Quantity: 10}
	writer := makeRequest("PUT", fmt.Sprintf("/api/v1/inventory/%s", p.ID), req, tokenForUser(&u))
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusForbidden, writer.Code)
	assert.Equal(t, "Permission denied", response["error"]["message"])
}

func TestInventoryAPI_AdjustStockForbiddenForCustomer(t *testing.T) {
	u := userModel.User{
		Email:    "customer-inventory-adjust@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "test-product-inventory-adjust",
		Description: "test-product-inventory-adjust",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.AdjustInventoryReq{QuantityDelta: 10}
	writer := makeRequest("PATCH", fmt.Sprintf("/api/v1/inventory/%s/adjust", p.ID), req, tokenForUser(&u))
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusForbidden, writer.Code)
	assert.Equal(t, "Permission denied", response["error"]["message"])
}
