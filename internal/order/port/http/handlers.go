package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"

	inventoryService "goshop/internal/inventory/service"
	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	"goshop/pkg/config"
	"goshop/pkg/paging"
	"goshop/pkg/redis"
	"goshop/pkg/response"
	"goshop/pkg/utils"
)

const (
	idempotencyHeader           = "Idempotency-Key"
	idempotencyStatusProcessing = "processing"
	idempotencyStatusSucceeded  = "succeeded"
)

type orderService interface {
	PlaceOrder(ctx context.Context, req *dto.PlaceOrderReq) (*model.Order, error)
	GetOrderByID(ctx context.Context, id, userID string) (*model.Order, error)
	GetMyOrders(ctx context.Context, req *dto.ListOrderReq) ([]*model.Order, *paging.Pagination, error)
	CancelOrder(ctx context.Context, orderID, userID string) (*model.Order, error)
}

type OrderHandler struct {
	service         orderService
	cache           redis.IRedis
	idempotencyTTL  time.Duration
	rateLimitLimit  int64
	rateLimitWindow time.Duration
}

type OrderHandlerOption func(*OrderHandler)

func WithOrderCache(cache redis.IRedis) OrderHandlerOption {
	return func(handler *OrderHandler) {
		handler.cache = cache
	}
}

func WithOrderIdempotencyTTL(ttl time.Duration) OrderHandlerOption {
	return func(handler *OrderHandler) {
		handler.idempotencyTTL = ttl
	}
}

func WithOrderRateLimit(limit int64, window time.Duration) OrderHandlerOption {
	return func(handler *OrderHandler) {
		handler.rateLimitLimit = limit
		handler.rateLimitWindow = window
	}
}

func NewOrderHandler(service orderService, opts ...OrderHandlerOption) *OrderHandler {
	handler := &OrderHandler{
		service:         service,
		idempotencyTTL:  config.OrderIdempotencyTTL(),
		rateLimitLimit:  config.OrderRateLimitLimit(),
		rateLimitWindow: config.OrderRateLimitWindow(),
	}
	for _, opt := range opts {
		opt(handler)
	}

	return handler
}

type idempotencyRecord struct {
	Status  string `json:"status"`
	OrderID string `json:"order_id,omitempty"`
}

// PlaceOrder godoc
//
//	@Summary	place order
//	@Tags		orders
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		_	body		dto.PlaceOrderReq	true	"Body"
//	@Success	200	{object}	dto.Order
//	@Router		/api/v1/orders [post]
func (a *OrderHandler) PlaceOrder(c *gin.Context) {
	startedAt := time.Now()
	var req dto.PlaceOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Failed to get body", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	req.UserID = c.GetString("userId")
	if req.UserID == "" {
		response.Error(c, http.StatusUnauthorized, errors.New("unauthorized"), "Unauthorized")
		return
	}

	if !a.allowOrderPlacement(c, req.UserID) {
		logger.Errorf("order_place_rate_limited user_id=%s", req.UserID)
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader(idempotencyHeader))
	cacheKey, acquired, handled := a.beginIdempotentOrder(c, req.UserID, idempotencyKey)
	if handled {
		logger.Errorf("order_place_idempotency_duplicate user_id=%s latency_ms=%d", req.UserID, time.Since(startedAt).Milliseconds())
		return
	}
	if cacheKey != "" && !acquired {
		response.Error(c, http.StatusConflict, errors.New("duplicate request"), "Duplicate request")
		return
	}

	order, err := a.service.PlaceOrder(c, &req)
	if err != nil {
		a.releaseIdempotencyOnFailure(cacheKey)
		status, message := orderPlacementErrorResponse(err)
		logger.Errorf("order_place_failed user_id=%s category=%s latency_ms=%d error=%s", req.UserID, message, time.Since(startedAt).Milliseconds(), err)
		response.Error(c, status, err, message)
		return
	}

	a.completeIdempotentOrder(cacheKey, order.ID)

	var res dto.Order
	utils.Copy(&res, &order)
	response.JSON(c, http.StatusOK, res)
}

func (a *OrderHandler) beginIdempotentOrder(c *gin.Context, userID, idempotencyKey string) (string, bool, bool) {
	if a.cache == nil || idempotencyKey == "" {
		return "", false, false
	}

	cacheKey := orderIdempotencyRedisKey(userID, idempotencyKey)
	acquired, err := a.cache.SetNXWithExpiration(cacheKey, idempotencyRecord{Status: idempotencyStatusProcessing}, a.idempotencyTTL)
	if err != nil {
		logger.Errorf("order_place_idempotency_start_failed user_id=%s error=%s", userID, err)
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return cacheKey, false, true
	}
	if acquired {
		return cacheKey, true, false
	}

	var record idempotencyRecord
	if err := a.cache.Get(cacheKey, &record); err != nil {
		logger.Errorf("order_place_idempotency_lookup_failed user_id=%s error=%s", userID, err)
		response.Error(c, http.StatusConflict, err, "Duplicate request")
		return cacheKey, false, true
	}

	if record.Status != idempotencyStatusSucceeded || record.OrderID == "" {
		response.Error(c, http.StatusConflict, errors.New("duplicate request in progress"), "Duplicate request in progress")
		return cacheKey, false, true
	}

	order, err := a.service.GetOrderByID(c, record.OrderID, userID)
	if err != nil {
		logger.Errorf("order_place_idempotency_order_lookup_failed user_id=%s order_id=%s error=%s", userID, record.OrderID, err)
		response.Error(c, http.StatusConflict, err, "Duplicate request")
		return cacheKey, false, true
	}

	var res dto.Order
	utils.Copy(&res, &order)
	response.JSON(c, http.StatusOK, res)
	return cacheKey, false, true
}

func (a *OrderHandler) completeIdempotentOrder(cacheKey, orderID string) {
	if a.cache == nil || cacheKey == "" || orderID == "" {
		return
	}

	err := a.cache.SetWithExpiration(
		cacheKey,
		idempotencyRecord{Status: idempotencyStatusSucceeded, OrderID: orderID},
		a.idempotencyTTL,
	)
	if err != nil {
		logger.Errorf("order_place_idempotency_complete_failed order_id=%s error=%s", orderID, err)
	}
}

func (a *OrderHandler) releaseIdempotencyOnFailure(cacheKey string) {
	if a.cache == nil || cacheKey == "" {
		return
	}
	if err := a.cache.Remove(cacheKey); err != nil {
		logger.Errorf("order_place_idempotency_release_failed error=%s", err)
	}
}

func (a *OrderHandler) allowOrderPlacement(c *gin.Context, userID string) bool {
	if a.cache == nil || a.rateLimitLimit <= 0 {
		return true
	}

	count, err := a.cache.IncrementWithExpiration(orderRateLimitRedisKey(userID), a.rateLimitWindow)
	if err != nil {
		logger.Errorf("order_place_rate_limit_error user_id=%s error=%s", userID, err)
		return true
	}
	if count <= a.rateLimitLimit {
		return true
	}

	response.Error(c, http.StatusTooManyRequests, errors.New("rate limited"), "Too many requests")
	return false
}

func orderPlacementErrorResponse(err error) (int, string) {
	if errors.Is(err, inventoryService.ErrInsufficientStock) {
		return http.StatusConflict, "Insufficient stock"
	}

	return http.StatusInternalServerError, "Something went wrong"
}

func orderIdempotencyRedisKey(userID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(idempotencyKey))
	return fmt.Sprintf("idempotency:orders:%s:%s", userID, hex.EncodeToString(sum[:]))
}

func orderRateLimitRedisKey(userID string) string {
	return fmt.Sprintf("rate-limit:orders:%s", userID)
}

// GetOrders godoc
//
//	@Summary	get my orders
//	@Tags		orders
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		_	query		dto.ListOrderReq	true	"Query"
//	@Success	200	{object}	dto.ListOrderRes
//	@Router		/api/v1/orders [get]
func (a *OrderHandler) GetOrders(c *gin.Context) {
	var req dto.ListOrderReq
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error("Failed to parse request req: ", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	req.UserID = c.GetString("userId")
	if req.UserID == "" {
		response.Error(c, http.StatusUnauthorized, errors.New("unauthorized"), "Unauthorized")
		return
	}

	orders, pagination, err := a.service.GetMyOrders(c, &req)
	if err != nil {
		logger.Error("Failed to get orders: ", err)
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.ListOrderRes
	res.Pagination = pagination
	utils.Copy(&res.Orders, &orders)
	response.JSON(c, http.StatusOK, res)
}

// GetOrderByID godoc
//
//	@Summary	get order details
//	@Tags		orders
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		id	path		string	true	"Order ID"
//	@Success	200	{object}	dto.Order
//	@Router		/api/v1/orders/{id} [get]
func (a *OrderHandler) GetOrderByID(c *gin.Context) {
	userId := c.GetString("userId")
	if userId == "" {
		response.Error(c, http.StatusUnauthorized, errors.New("unauthorized"), "Unauthorized")
		return
	}

	orderId := c.Param("id")
	if orderId == "" {
		response.Error(c, http.StatusBadRequest, errors.New("bad request"), "Miss Order ID")
		return
	}

	order, err := a.service.GetOrderByID(c, orderId, userId)
	if err != nil {
		logger.Errorf("Failed to get order, id: %s, error: %s ", orderId, err)
		if errors.Is(err, model.ErrPermissionDenied) {
			response.Error(c, http.StatusForbidden, err, "Permission denied")
			return
		}
		response.Error(c, http.StatusNotFound, err, "Not found")
		return
	}

	var res dto.Order
	utils.Copy(&res, &order)
	response.JSON(c, http.StatusOK, res)
}

// CancelOrder godoc
//
//	@Summary	cancel order
//	@Tags		orders
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		id	path	string	true	"Order ID"
//	@Router		/api/v1/orders/{id}/cancel [put]
func (a *OrderHandler) CancelOrder(c *gin.Context) {
	userID := c.GetString("userId")
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, errors.New("unauthorized"), "Unauthorized")
		return
	}

	orderID := c.Param("id")
	if orderID == "" {
		response.Error(c, http.StatusBadRequest, errors.New("bad request"), "Miss Order ID")
		return
	}

	order, err := a.service.CancelOrder(c, orderID, userID)
	if err != nil {
		logger.Errorf("Failed to cancel order, id: %s, error: %s", orderID, err)
		if errors.Is(err, model.ErrPermissionDenied) {
			response.Error(c, http.StatusForbidden, err, "Permission denied")
			return
		}
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.Order
	utils.Copy(&res, &order)
	response.JSON(c, http.StatusOK, res)
}
