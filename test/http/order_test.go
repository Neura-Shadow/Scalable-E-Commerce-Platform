package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	inventoryModel "goshop/internal/inventory/model"
	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	outboxModel "goshop/internal/outbox/model"
	productModel "goshop/internal/product/model"
	userModel "goshop/internal/user/model"
	"goshop/pkg/jtoken"
)

// Place Order
// =================================================================================================

func TestOrderAPI_PlaceOrderSuccess(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, accessToken())
	var res dto.Order
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, "new", res.Status)
	assert.Equal(t, float64(8), res.TotalPrice)
	assert.Equal(t, 2, len(res.Lines))
	assert.Equal(t, req.Lines[0].ProductID, res.Lines[0].Product.ID)
	assert.Equal(t, req.Lines[0].Quantity, res.Lines[0].Quantity)
	assert.Equal(t, float64(2), res.Lines[0].Price)

	assert.Equal(t, req.Lines[1].ProductID, res.Lines[1].Product.ID)
	assert.Equal(t, req.Lines[1].Quantity, res.Lines[1].Quantity)
	assert.Equal(t, float64(6), res.Lines[1].Price)
}

func TestOrderAPI_PlaceOrderCreatesOutboxEvent(t *testing.T) {
	defer cleanData()

	u := userModel.User{
		Email:    "outbox-customer@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "outbox-product",
		Description: "outbox-product",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  3,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, tokenForUser(&u))
	var order dto.Order
	parseResponseResult(writer.Body.Bytes(), &order)
	assert.Equal(t, http.StatusOK, writer.Code)

	var event outboxModel.OutboxEvent
	err := dbTest.GetDB().
		Where("aggregate_type = ? AND aggregate_id = ? AND event_type = ?", "order", order.ID, "order.created").
		First(&event).Error
	assert.NoError(t, err)
	assert.Equal(t, outboxModel.OutboxEventStatusPending, event.Status)
	assert.Equal(t, 0, event.Attempts)
	assert.Nil(t, event.PublishedAt)

	var payload struct {
		OrderID    string  `json:"order_id"`
		UserID     string  `json:"user_id"`
		TotalPrice float64 `json:"total_price"`
		Status     string  `json:"status"`
		Lines      []struct {
			ProductID string  `json:"product_id"`
			Quantity  uint    `json:"quantity"`
			Price     float64 `json:"price"`
		} `json:"lines"`
	}
	assert.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, order.ID, payload.OrderID)
	assert.Equal(t, u.ID, payload.UserID)
	assert.Equal(t, float64(6), payload.TotalPrice)
	assert.Equal(t, "new", payload.Status)
	assert.Len(t, payload.Lines, 1)
	assert.Equal(t, p.ID, payload.Lines[0].ProductID)
	assert.Equal(t, uint(3), payload.Lines[0].Quantity)
	assert.Equal(t, float64(6), payload.Lines[0].Price)
}

func TestOrderAPI_InventoryFailureDoesNotCommitOutboxEvent(t *testing.T) {
	defer cleanData()

	u := userModel.User{
		Email:    "outbox-no-stock@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "outbox-no-stock-product",
		Description: "outbox-no-stock-product",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p)
	dbTest.GetDB().
		Model(&inventoryModel.Inventory{}).
		Where("product_id = ?", p.ID).
		Update("quantity", int64(0))

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, tokenForUser(&u))
	assert.Equal(t, http.StatusConflict, writer.Code)

	var outboxCount int64
	dbTest.GetDB().Model(&outboxModel.OutboxEvent{}).Where("event_type = ?", "order.created").Count(&outboxCount)
	assert.Equal(t, int64(0), outboxCount)

	var orderCount int64
	dbTest.GetDB().Model(&model.Order{}).Where("user_id = ?", u.ID).Count(&orderCount)
	assert.Equal(t, int64(0), orderCount)
}

func TestOrderAPI_PlaceOrderInvalidFieldType(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := map[string]interface{}{
		"lines": []map[string]interface{}{
			{
				"product_id": p1.ID,
				"quantity":   2,
			},
			{
				"product_id": p2.ID,
				"quantity":   "1",
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Equal(t, "Invalid parameters", response["error"]["message"])
}

func TestOrderAPI_PlaceOrderInvalidMissProductID(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				Quantity: 2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_PlaceOrderInvalidMissQuantity(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_PlaceOrderInvalidProductNotFound(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: "notfound",
				Quantity:  1,
			},
		},
	}
	writer := makeRequest("POST", "/api/v1/orders", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_PlaceOrderUnauthorized(t *testing.T) {
	defer cleanData()

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
	}

	writer := makeRequest("POST", "/api/v1/orders", req, "")
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

func TestOrderAPI_IdempotentPlaceOrderDuplicateReturnsSameOrder(t *testing.T) {
	defer cleanData()

	u := userModel.User{
		Email:    "idempotent-customer@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "idempotent-product",
		Description: "idempotent-product",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	headers := map[string]string{"Idempotency-Key": "checkout-duplicate"}
	token := tokenForUser(&u)

	first := makeRequestWithHeaders("POST", "/api/v1/orders", req, token, headers)
	second := makeRequestWithHeaders("POST", "/api/v1/orders", req, token, headers)

	var firstOrder dto.Order
	var secondOrder dto.Order
	parseResponseResult(first.Body.Bytes(), &firstOrder)
	parseResponseResult(second.Body.Bytes(), &secondOrder)
	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Equal(t, firstOrder.ID, secondOrder.ID)

	var orderCount int64
	dbTest.GetDB().Model(&model.Order{}).Where("user_id = ?", u.ID).Count(&orderCount)
	assert.Equal(t, int64(1), orderCount)
}

func TestOrderAPI_IdempotencyKeyScopedByUser(t *testing.T) {
	defer cleanData()

	u1 := userModel.User{
		Email:    "idempotent-user-1@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	u2 := userModel.User{
		Email:    "idempotent-user-2@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u1)
	dbTest.Create(context.Background(), &u2)
	defer cleanData(&u1, &u2)

	p := productModel.Product{
		Name:        "idempotent-scoped-product",
		Description: "idempotent-scoped-product",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	headers := map[string]string{"Idempotency-Key": "same-client-key"}

	first := makeRequestWithHeaders("POST", "/api/v1/orders", req, tokenForUser(&u1), headers)
	second := makeRequestWithHeaders("POST", "/api/v1/orders", req, tokenForUser(&u2), headers)

	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusOK, second.Code)

	var orderCount int64
	dbTest.GetDB().Model(&model.Order{}).Where("user_id IN ?", []string{u1.ID, u2.ID}).Count(&orderCount)
	assert.Equal(t, int64(2), orderCount)
}

func TestOrderAPI_MissingIdempotencyKeyAllowsSeparateOrders(t *testing.T) {
	defer cleanData()

	u := userModel.User{
		Email:    "no-idempotency-customer@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "no-idempotency-product",
		Description: "no-idempotency-product",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	token := tokenForUser(&u)

	first := makeRequest("POST", "/api/v1/orders", req, token)
	second := makeRequest("POST", "/api/v1/orders", req, token)

	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusOK, second.Code)

	var orderCount int64
	dbTest.GetDB().Model(&model.Order{}).Where("user_id = ?", u.ID).Count(&orderCount)
	assert.Equal(t, int64(2), orderCount)
}

func TestOrderAPI_ConcurrentOrdersNeverOversell(t *testing.T) {
	defer cleanData()

	u := userModel.User{
		Email:    "limited-stock-customer@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p := productModel.Product{
		Name:        "limited-stock-product",
		Description: "limited-stock-product",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)
	dbTest.GetDB().
		Model(&inventoryModel.Inventory{}).
		Where("product_id = ?", p.ID).
		Update("quantity", int64(3))

	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	token := tokenForUser(&u)

	var wg sync.WaitGroup
	var successCount int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writer := makeRequest("POST", "/api/v1/orders", req, token)
			if writer.Code == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	var inventory inventoryModel.Inventory
	err := dbTest.GetDB().Where("product_id = ?", p.ID).First(&inventory).Error
	assert.NoError(t, err)
	assert.Equal(t, int64(3), successCount)
	assert.Equal(t, int64(0), inventory.Quantity)
	assert.GreaterOrEqual(t, inventory.Quantity, int64(0))

	var orderCount int64
	dbTest.GetDB().Model(&model.Order{}).Where("user_id = ?", u.ID).Count(&orderCount)
	assert.Equal(t, int64(3), orderCount)
}

// Get Order Detail
// =================================================================================================

func TestOrderAPI_GetOrderByIDSuccess(t *testing.T) {
	u := userModel.User{
		Email:    "test1@gmail.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("GET", fmt.Sprintf("/api/v1/orders/%s", o.ID), nil, token)
	var res dto.Order
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, "new", res.Status)
	assert.Equal(t, 2, len(res.Lines))
	assert.Equal(t, o.Lines[0].ProductID, res.Lines[0].Product.ID)
	assert.Equal(t, o.Lines[0].Quantity, res.Lines[0].Quantity)

	assert.Equal(t, o.Lines[1].ProductID, res.Lines[1].Product.ID)
	assert.Equal(t, o.Lines[1].Quantity, res.Lines[1].Quantity)
}

func TestOrderAPI_GetOrderByIDNotFound(t *testing.T) {
	writer := makeRequest("GET", "/api/v1/orders/notfound", nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusNotFound, writer.Code)
	assert.Equal(t, "Not found", response["error"]["message"])
}

func TestOrderAPI_GetOrderByIDNotMine(t *testing.T) {
	u := userModel.User{
		Email:    "order-owner@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	otherUser := userModel.User{
		Email:    "order-viewer@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleCustomer,
	}
	dbTest.Create(context.Background(), &otherUser)
	defer cleanData(&otherUser)

	p := productModel.Product{
		Name:        "test-product-not-mine",
		Description: "test-product-not-mine",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p.ID,
				Quantity:  1,
			},
		},
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("GET", fmt.Sprintf("/api/v1/orders/%s", o.ID), nil, tokenForUser(&otherUser))
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusForbidden, writer.Code)
	assert.Equal(t, "Permission denied", response["error"]["message"])
}

// Cancel Order
// =================================================================================================

func TestOrderAPI_CancelOrderSuccess(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("PUT", fmt.Sprintf("/api/v1/orders/%s/cancel", o.ID), nil, token)
	assert.Equal(t, http.StatusOK, writer.Code)
}

func TestOrderAPI_CancelOrderNotFound(t *testing.T) {
	writer := makeRequest("PUT", "/api/v1/orders/notfound/cancel", nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_CancelOrderStatusDone(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("PUT", fmt.Sprintf("/api/v1/orders/%s/cancel", o.ID), nil, token)
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_CancelOrderStatusCancelled(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusCancelled,
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("PUT", fmt.Sprintf("/api/v1/orders/%s/cancel", o.ID), nil, token)
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestOrderAPI_CancelOrderNotMine(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  2,
			},
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o)

	writer := makeRequest("PUT", fmt.Sprintf("/api/v1/orders/%s/cancel", o.ID), nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusForbidden, writer.Code)
	assert.Equal(t, "Permission denied", response["error"]["message"])
}

// List My Orders
// =================================================================================================

func TestOrderAPI_ListOrdersSuccess(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, int64(2), res.Pagination.Total)
	assert.Equal(t, int64(1), res.Pagination.CurrentPage)
	assert.Equal(t, int64(1), res.Pagination.TotalPage)
	assert.Equal(t, int64(20), res.Pagination.Limit)
	assert.Equal(t, 2, len(res.Orders))
	assert.Equal(t, o1.Lines[0].ProductID, res.Orders[0].Lines[0].Product.ID)
	assert.Equal(t, o1.Lines[0].Quantity, res.Orders[0].Lines[0].Quantity)

	assert.Equal(t, o2.Lines[0].ProductID, res.Orders[1].Lines[0].Product.ID)
	assert.Equal(t, o2.Lines[0].Quantity, res.Orders[1].Lines[0].Quantity)
}

func TestOrderAPI_ListOrdersNotFound(t *testing.T) {
	defer cleanData()

	writer := makeRequest("GET", "/api/v1/orders", nil, accessToken())
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, 0, len(res.Orders))
}

func TestOrderAPI_ListOrdersInvalidFieldType(t *testing.T) {
	writer := makeRequest("GET", "/api/v1/orders?page=a", nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Equal(t, "Invalid parameters", response["error"]["message"])
}

func TestOrderAPI_ListMyOrdersFindByStatusSuccess(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?status=new", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, int64(1), res.Pagination.Total)
	assert.Equal(t, int64(1), res.Pagination.CurrentPage)
	assert.Equal(t, int64(1), res.Pagination.TotalPage)
	assert.Equal(t, int64(20), res.Pagination.Limit)
	assert.Equal(t, 1, len(res.Orders))
	assert.Equal(t, o2.Lines[0].ProductID, res.Orders[0].Lines[0].Product.ID)
	assert.Equal(t, o2.Lines[0].Quantity, res.Orders[0].Lines[0].Quantity)
}

func TestOrderAPI_ListOrdersFindByStatusNotFound(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?status=cancelled", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, 0, len(res.Orders))
}

func TestOrderAPI_ListOrdersFindByCodeSuccess(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", fmt.Sprintf("/api/v1/orders?code=%s", o1.Code), nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, int64(1), res.Pagination.Total)
	assert.Equal(t, int64(1), res.Pagination.CurrentPage)
	assert.Equal(t, int64(1), res.Pagination.TotalPage)
	assert.Equal(t, int64(20), res.Pagination.Limit)
	assert.Equal(t, 1, len(res.Orders))
	assert.Equal(t, o1.Lines[0].ProductID, res.Orders[0].Lines[0].Product.ID)
	assert.Equal(t, o1.Lines[0].Quantity, res.Orders[0].Lines[0].Quantity)
}

func TestOrderAPI_ListOrdersFindByCodeNotFound(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?code=notfound", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, 0, len(res.Orders))
}

func TestOrderAPI_ListOrdersWithPagination(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?page=2&limit=1", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, int64(2), res.Pagination.Total)
	assert.Equal(t, int64(2), res.Pagination.CurrentPage)
	assert.Equal(t, int64(2), res.Pagination.TotalPage)
	assert.Equal(t, int64(1), res.Pagination.Limit)
	assert.Equal(t, 1, len(res.Orders))
	assert.Equal(t, o2.Lines[0].ProductID, res.Orders[0].Lines[0].Product.ID)
	assert.Equal(t, o2.Lines[0].Quantity, res.Orders[0].Lines[0].Quantity)
}

func TestOrderAPI_ListOrdersWithOrder(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": u.ID})
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?order_by=created_at&order_desc=true", nil, token)
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, int64(2), res.Pagination.Total)
	assert.Equal(t, int64(1), res.Pagination.CurrentPage)
	assert.Equal(t, int64(1), res.Pagination.TotalPage)
	assert.Equal(t, int64(20), res.Pagination.Limit)
	assert.Equal(t, 2, len(res.Orders))
	assert.Equal(t, o2.Lines[0].ProductID, res.Orders[0].Lines[0].Product.ID)
	assert.Equal(t, o2.Lines[0].Quantity, res.Orders[0].Lines[0].Quantity)

	assert.Equal(t, o1.Lines[0].ProductID, res.Orders[1].Lines[0].Product.ID)
	assert.Equal(t, o1.Lines[0].Quantity, res.Orders[1].Lines[0].Quantity)
}

func TestOrderAPI_GetMyOrdersNotMine(t *testing.T) {
	u := userModel.User{
		Email:    "test1@test.com",
		Password: "test123456",
	}
	dbTest.Create(context.Background(), &u)
	defer cleanData(&u)

	p1 := productModel.Product{
		Name:        "test-product-1",
		Description: "test-product-1",
		Price:       1,
	}
	dbTest.Create(context.Background(), &p1)

	p2 := productModel.Product{
		Name:        "test-product-2",
		Description: "test-product-2",
		Price:       2,
	}
	dbTest.Create(context.Background(), &p2)

	o1 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p1.ID,
				Quantity:  5,
			},
		},
		Status: model.OrderStatusDone,
	}
	dbTest.Create(context.Background(), &o1)

	o2 := model.Order{
		UserID: u.ID,
		Lines: []*model.OrderLine{
			{
				ProductID: p2.ID,
				Quantity:  3,
			},
		},
		Status: model.OrderStatusNew,
	}
	dbTest.Create(context.Background(), &o2)

	writer := makeRequest("GET", "/api/v1/orders?code=notfound", nil, accessToken())
	var res dto.ListOrderRes
	parseResponseResult(writer.Body.Bytes(), &res)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, 0, len(res.Orders))
}
