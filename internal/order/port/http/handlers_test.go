package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	inventoryService "goshop/internal/inventory/service"
	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	"goshop/internal/order/service/mocks"
	productMocks "goshop/internal/product/service/mocks"
	"goshop/pkg/config"
	appMetrics "goshop/pkg/metrics"
	"goshop/pkg/paging"
	redisMocks "goshop/pkg/redis/mocks"
	"goshop/pkg/response"
	"goshop/pkg/utils"
)

type OrderHandlerTestSuite struct {
	suite.Suite
	mockService        *mocks.IOrderService
	mockProductService *productMocks.IProductService
	handler            *OrderHandler
}

func (suite *OrderHandlerTestSuite) SetupTest() {
	logger.Initialize(config.ProductionEnv)

	suite.mockService = mocks.NewIOrderService(suite.T())
	suite.mockProductService = productMocks.NewIProductService(suite.T())
	suite.handler = NewOrderHandler(suite.mockService)
}

func TestOrderHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(OrderHandlerTestSuite))
}

func (suite *OrderHandlerTestSuite) prepareContext(body any) (*gin.Context, *httptest.ResponseRecorder) {
	requestBody, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("", "/", bytes.NewBuffer(requestBody))
	c, _ := gin.CreateTestContext(w)
	c.Request = r

	return c, w
}

// PlaceOrder
// =================================================================================================

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderSuccess() {
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
			{
				ProductID: "productId2",
				Quantity:  3,
			},
		},
	}

	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	req.UserID = "123456"

	suite.mockService.On("PlaceOrder", mock.Anything, req).
		Return(
			&model.Order{
				ID:         "orderId1",
				Code:       "orderCode1",
				TotalPrice: 8,
				Status:     model.OrderStatusNew,
				Lines: []*model.OrderLine{
					{
						ProductID: "productId1",
						Quantity:  2,
					},
					{
						ProductID: "productId2",
						Quantity:  3,
					},
				},
			},
			nil,
		).Times(1)

	suite.handler.PlaceOrder(ctx)

	var res response.Response
	var orderRes dto.Order

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&orderRes, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(float64(8), orderRes.TotalPrice)
	suite.Equal(string(model.OrderStatusNew), orderRes.Status)
	suite.Equal(2, len(orderRes.Lines))
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderFirstIdempotentRequestSuccess() {
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
		},
	}
	cache := redisMocks.NewIRedis(suite.T())
	handler := NewOrderHandler(
		suite.mockService,
		WithOrderCache(cache),
		WithOrderRateLimit(0, time.Minute),
		WithOrderIdempotencyTTL(time.Hour),
	)
	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	ctx.Request.Header.Set("Idempotency-Key", "checkout-123")
	req.UserID = "123456"
	req.IdempotencyKeyHash = orderIdempotencyKeyHash("checkout-123")
	req.IdempotencyFingerprint = orderRequestFingerprint(req.Lines)
	cacheKey := orderIdempotencyRedisKey("123456", "checkout-123")

	cache.On("SetNXWithExpiration", cacheKey, mock.MatchedBy(func(record idempotencyRecord) bool {
		return record.Status == idempotencyStatusProcessing &&
			record.OrderID == "" &&
			record.Fingerprint == req.IdempotencyFingerprint
	}), time.Hour).Return(true, nil).Times(1)
	suite.mockService.On("PlaceOrder", mock.Anything, req).
		Return(&model.Order{
			ID:         "orderId1",
			Code:       "orderCode1",
			UserID:     "123456",
			TotalPrice: 2,
			Status:     model.OrderStatusNew,
			Lines: []*model.OrderLine{
				{
					ProductID: "productId1",
					Quantity:  2,
				},
			},
		}, nil).Times(1)
	cache.On("SetWithExpiration", cacheKey, mock.MatchedBy(func(record idempotencyRecord) bool {
		return record.Status == idempotencyStatusSucceeded &&
			record.OrderID == "orderId1" &&
			record.Fingerprint == req.IdempotencyFingerprint
	}), time.Hour).Return(nil).Times(1)

	handler.PlaceOrder(ctx)

	suite.Equal(http.StatusOK, writer.Code)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderDuplicateIdempotencyReturnsStoredOrder() {
	appMetrics.ResetForTest()
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
		},
	}
	cache := redisMocks.NewIRedis(suite.T())
	handler := NewOrderHandler(
		suite.mockService,
		WithOrderCache(cache),
		WithOrderRateLimit(0, time.Minute),
		WithOrderIdempotencyTTL(time.Hour),
	)
	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	ctx.Request.Header.Set("Idempotency-Key", "checkout-123")
	fingerprint := orderRequestFingerprint(req.Lines)
	cacheKey := orderIdempotencyRedisKey("123456", "checkout-123")

	cache.On("SetNXWithExpiration", cacheKey, mock.Anything, time.Hour).Return(false, nil).Times(1)
	cache.On("Get", cacheKey, mock.AnythingOfType("*http.idempotencyRecord")).
		Run(func(args mock.Arguments) {
			record := args.Get(1).(*idempotencyRecord)
			record.Status = idempotencyStatusSucceeded
			record.OrderID = "orderId1"
			record.Fingerprint = fingerprint
		}).
		Return(nil).Times(1)
	suite.mockService.On("GetOrderByID", mock.Anything, "orderId1", "123456").
		Return(&model.Order{
			ID:         "orderId1",
			Code:       "orderCode1",
			UserID:     "123456",
			TotalPrice: 2,
			Status:     model.OrderStatusNew,
		}, nil).Times(1)

	handler.PlaceOrder(ctx)

	snapshot, err := appMetrics.SnapshotText()
	suite.NoError(err)
	var res response.Response
	var orderRes dto.Order
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&orderRes, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal("orderId1", orderRes.ID)
	suite.Contains(snapshot, "order_idempotency_duplicate_total")
	suite.Contains(snapshot, `reason="replay"`)
	suite.NotContains(snapshot, "123456")
	suite.NotContains(snapshot, "orderId1")
	suite.NotContains(snapshot, "checkout-123")
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderRejectsIdempotencyFingerprintMismatch() {
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{{ProductID: "productId1", Quantity: 2}},
	}
	cache := redisMocks.NewIRedis(suite.T())
	handler := NewOrderHandler(
		suite.mockService,
		WithOrderCache(cache),
		WithOrderRateLimit(0, time.Minute),
		WithOrderIdempotencyTTL(time.Hour),
	)
	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	ctx.Request.Header.Set("Idempotency-Key", "checkout-123")
	cacheKey := orderIdempotencyRedisKey("123456", "checkout-123")

	cache.On("SetNXWithExpiration", cacheKey, mock.Anything, time.Hour).Return(false, nil).Once()
	cache.On("Get", cacheKey, mock.AnythingOfType("*http.idempotencyRecord")).
		Run(func(args mock.Arguments) {
			record := args.Get(1).(*idempotencyRecord)
			record.Status = idempotencyStatusSucceeded
			record.OrderID = "orderId1"
			record.Fingerprint = "different-request"
		}).
		Return(nil).Once()

	handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusConflict, writer.Code)
	suite.Equal("Idempotency key conflict", res["error"]["message"])
	suite.mockService.AssertNotCalled(suite.T(), "PlaceOrder", mock.Anything, mock.Anything)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderDuplicateIdempotencyInFlightReturnsConflict() {
	appMetrics.ResetForTest()
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
		},
	}
	cache := redisMocks.NewIRedis(suite.T())
	handler := NewOrderHandler(
		suite.mockService,
		WithOrderCache(cache),
		WithOrderRateLimit(0, time.Minute),
		WithOrderIdempotencyTTL(time.Hour),
	)
	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	ctx.Request.Header.Set("Idempotency-Key", "checkout-123")
	fingerprint := orderRequestFingerprint(req.Lines)
	cacheKey := orderIdempotencyRedisKey("123456", "checkout-123")

	cache.On("SetNXWithExpiration", cacheKey, mock.Anything, time.Hour).Return(false, nil).Times(1)
	cache.On("Get", cacheKey, mock.AnythingOfType("*http.idempotencyRecord")).
		Run(func(args mock.Arguments) {
			record := args.Get(1).(*idempotencyRecord)
			record.Status = idempotencyStatusProcessing
			record.Fingerprint = fingerprint
		}).
		Return(nil).Times(1)

	handler.PlaceOrder(ctx)

	snapshot, err := appMetrics.SnapshotText()
	suite.NoError(err)
	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusConflict, writer.Code)
	suite.Equal("Duplicate request in progress", res["error"]["message"])
	suite.Contains(snapshot, "order_idempotency_duplicate_total")
	suite.Contains(snapshot, `reason="in_flight"`)
	suite.Contains(snapshot, `reason="duplicate_in_flight"`)
	suite.NotContains(snapshot, "123456")
	suite.NotContains(snapshot, "checkout-123")
	suite.mockService.AssertNotCalled(suite.T(), "PlaceOrder", mock.Anything, mock.Anything)
	suite.mockService.AssertNotCalled(suite.T(), "GetOrderByID", mock.Anything, mock.Anything, mock.Anything)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderRateLimited() {
	appMetrics.ResetForTest()
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
		},
	}
	cache := redisMocks.NewIRedis(suite.T())
	handler := NewOrderHandler(
		suite.mockService,
		WithOrderCache(cache),
		WithOrderRateLimit(1, time.Minute),
	)
	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")

	cache.On("IncrementWithExpiration", orderRateLimitRedisKey("123456"), time.Minute).
		Return(int64(2), nil).Times(1)

	handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	snapshot, err := appMetrics.SnapshotText()
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, writer.Code)
	suite.Equal("Too many requests", res["error"]["message"])
	suite.Contains(snapshot, "order_rate_limited_total")
	suite.Contains(snapshot, `reason="rate_limited"`)
	suite.NotContains(snapshot, "123456")
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderInvalidProductIdType() {
	req := map[string]interface{}{
		"lines": []map[string]interface{}{
			{
				"product_id": 1,
				"quantity":   2,
			},
		},
	}

	ctx, writer := suite.prepareContext(req)

	suite.handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.Equal("Invalid parameters", res["error"]["message"])
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderInvalidQuantityType() {
	req := map[string]interface{}{
		"lines": []map[string]interface{}{
			{
				"product_id": "productId1",
				"quantity":   "1",
			},
		},
	}

	ctx, writer := suite.prepareContext(req)

	suite.handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.Equal("Invalid parameters", res["error"]["message"])
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderUnauthorized() {
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
			{
				ProductID: "productId2",
				Quantity:  3,
			},
		},
	}

	ctx, writer := suite.prepareContext(req)

	suite.handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusUnauthorized, writer.Code)
	suite.Equal("Unauthorized", res["error"]["message"])
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderFail() {
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
			{
				ProductID: "productId2",
				Quantity:  3,
			},
		},
	}

	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	req.UserID = "123456"

	suite.mockService.On("PlaceOrder", mock.Anything, req).
		Return(nil, errors.New("error")).Times(1)

	suite.handler.PlaceOrder(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusInternalServerError, writer.Code)
	suite.Equal("Something went wrong", res["error"]["message"])
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_PlaceOrderInsufficientStockIncrementsMetrics() {
	appMetrics.ResetForTest()
	req := &dto.PlaceOrderReq{
		Lines: []dto.PlaceOrderLineReq{
			{
				ProductID: "productId1",
				Quantity:  2,
			},
		},
	}

	ctx, writer := suite.prepareContext(req)
	ctx.Set("userId", "123456")
	req.UserID = "123456"

	suite.mockService.On("PlaceOrder", mock.Anything, req).
		Return(nil, inventoryService.ErrInsufficientStock).Times(1)

	suite.handler.PlaceOrder(ctx)

	snapshot, err := appMetrics.SnapshotText()
	suite.NoError(err)
	suite.Equal(http.StatusConflict, writer.Code)
	suite.Contains(snapshot, "order_place_failed_total")
	suite.Contains(snapshot, `reason="insufficient_stock"`)
	suite.Contains(snapshot, "inventory_insufficient_stock_total")
	suite.NotContains(snapshot, "123456")
}

// Get Order Detail
// =================================================================================================

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetOrderByIDSuccess() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")
	ctx.AddParam("id", "orderId1")

	suite.mockService.On("GetOrderByID", mock.Anything, "orderId1", "123456").
		Return(
			&model.Order{
				ID:         "orderId1",
				UserID:     "123456",
				TotalPrice: 5,
				Status:     model.OrderStatusNew,
			},
			nil,
		).Times(1)

	suite.handler.GetOrderByID(ctx)

	var res response.Response
	var orderRes dto.Order

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&orderRes, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(float64(5), orderRes.TotalPrice)
	suite.Equal(string(model.OrderStatusNew), orderRes.Status)
	suite.Equal(0, len(orderRes.Lines))
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetOrderByIDMissID() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")

	suite.handler.GetOrderByID(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetOrderByIDUnauthorized() {
	ctx, writer := suite.prepareContext(nil)
	ctx.AddParam("id", "orderId1")

	suite.handler.GetOrderByID(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusUnauthorized, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetOrderByIDFail() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")
	ctx.AddParam("id", "orderId1")

	suite.mockService.On("GetOrderByID", mock.Anything, "orderId1", "123456").
		Return(nil, errors.New("error")).Times(1)

	suite.handler.GetOrderByID(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusNotFound, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetOrderByIDPermissionDenied() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")
	ctx.AddParam("id", "orderId1")

	suite.mockService.On("GetOrderByID", mock.Anything, "orderId1", "123456").
		Return(nil, model.ErrPermissionDenied).Times(1)

	suite.handler.GetOrderByID(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusForbidden, writer.Code)
	suite.Equal("Permission denied", res["error"]["message"])
}

// GetOrders
// =================================================================================================

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetMyOrdersSuccess() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")

	suite.mockService.On("GetMyOrders", mock.Anything, mock.Anything).
		Return(
			[]*model.Order{
				{
					ID:         "orderId1",
					UserID:     "123456",
					TotalPrice: 5,
					Status:     model.OrderStatusNew,
				},
			},
			&paging.Pagination{
				Total:       1,
				CurrentPage: 1,
			},
			nil,
		).Times(1)

	suite.handler.GetOrders(ctx)

	var res response.Response
	var orderRes dto.ListOrderRes

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&orderRes, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(1, len(orderRes.Orders))
	suite.Equal("orderId1", orderRes.Orders[0].ID)
	suite.Equal(float64(5), orderRes.Orders[0].TotalPrice)
	suite.Equal(string(model.OrderStatusNew), orderRes.Orders[0].Status)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetMyOrdersUnauthorized() {
	ctx, writer := suite.prepareContext(nil)

	suite.handler.GetOrders(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusUnauthorized, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetMyOrdersInvalidFieldType() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Request.URL, _ = url.Parse("?page=q")

	suite.handler.GetOrders(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_GetMyOrdersFail() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")

	suite.mockService.On("GetMyOrders", mock.Anything, mock.Anything).
		Return(nil, nil, errors.New("error")).Times(1)

	suite.handler.GetOrders(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusInternalServerError, writer.Code)
	suite.NotNil(res.Error)
}

// CancelOrder
// =================================================================================================

func (suite *OrderHandlerTestSuite) TestOrderAPI_CancelOrderSuccess() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")
	ctx.AddParam("id", "orderId1")

	suite.mockService.On("CancelOrder", mock.Anything, "orderId1", "123456").
		Return(
			&model.Order{
				ID:         "orderId1",
				UserID:     "123456",
				TotalPrice: 5,
				Status:     model.OrderStatusNew,
			},
			nil,
		).Times(1)

	suite.handler.CancelOrder(ctx)

	var res response.Response
	var orderRes dto.Order

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&orderRes, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal("orderId1", orderRes.ID)
	suite.Equal(float64(5), orderRes.TotalPrice)
	suite.Equal(string(model.OrderStatusNew), orderRes.Status)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_CancelOrderUnauthorized() {
	ctx, writer := suite.prepareContext(nil)
	ctx.AddParam("id", "orderId1")

	suite.handler.CancelOrder(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusUnauthorized, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_CancelOrderMissID() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")

	suite.handler.CancelOrder(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.NotNil(res.Error)
}

func (suite *OrderHandlerTestSuite) TestOrderAPI_CancelOrderFail() {
	ctx, writer := suite.prepareContext(nil)
	ctx.Set("userId", "123456")
	ctx.AddParam("id", "orderId1")

	suite.mockService.On("CancelOrder", mock.Anything, "orderId1", "123456").
		Return(nil, errors.New("error")).Times(1)

	suite.handler.CancelOrder(ctx)

	var res response.Response
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusInternalServerError, writer.Code)
	suite.NotNil(res.Error)
}
