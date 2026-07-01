package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"

	"goshop/internal/inventory/dto"
	"goshop/internal/inventory/service"
	"goshop/pkg/response"
	"goshop/pkg/utils"
)

type InventoryHandler struct {
	service service.IInventoryService
}

func NewInventoryHandler(service service.IInventoryService) *InventoryHandler {
	return &InventoryHandler{service: service}
}

// List godoc
//
//	@Summary	list inventory
//	@Tags		inventory
//	@Produce	json
//	@Param		product_id	query		string	false	"Product ID"
//	@Success	200			{object}	dto.ListInventoryRes
//	@Router		/api/v1/inventory [get]
func (h *InventoryHandler) List(c *gin.Context) {
	var req dto.ListInventoryReq
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error("Failed to parse query", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	items, pagination, err := h.service.List(c, &req)
	if err != nil {
		logger.Error("Failed to list inventory", err)
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.ListInventoryRes
	utils.Copy(&res.Items, &items)
	res.Pagination = pagination
	response.JSON(c, http.StatusOK, res)
}

// Get godoc
//
//	@Summary	get inventory by product id
//	@Tags		inventory
//	@Produce	json
//	@Param		product_id	path		string	true	"Product ID"
//	@Success	200			{object}	dto.Inventory
//	@Router		/api/v1/inventory/{product_id} [get]
func (h *InventoryHandler) Get(c *gin.Context) {
	productID := c.Param("product_id")
	item, err := h.service.GetByProductID(c, productID)
	if err != nil {
		logger.Error("Failed to get inventory", err)
		status := http.StatusInternalServerError
		message := "Something went wrong"
		if errors.Is(err, service.ErrInventoryNotFound) {
			status = http.StatusNotFound
			message = "Not found"
		}
		response.Error(c, status, err, message)
		return
	}

	var res dto.Inventory
	utils.Copy(&res, &item)
	response.JSON(c, http.StatusOK, res)
}

// Set godoc
//
//	@Summary	set inventory stock
//	@Description	Admin-only stock replacement for one product.
//	@Tags		inventory
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		product_id	path		string				true	"Product ID"
//	@Param		_			body		dto.SetInventoryReq	true	"Body"
//	@Success	200			{object}	dto.Inventory
//	@Failure	403			{object}	response.Response	"Permission denied"
//	@Router		/api/v1/inventory/{product_id} [put]
func (h *InventoryHandler) Set(c *gin.Context) {
	productID := c.Param("product_id")
	var req dto.SetInventoryReq
	if err := c.ShouldBindJSON(&req); c.Request.Body == nil || err != nil {
		logger.Error("Failed to parse body", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}
	req.ProductID = productID

	item, err := h.service.SetStock(c, &req)
	if err != nil {
		logger.Error("Failed to set inventory", err)
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.Inventory
	utils.Copy(&res, &item)
	response.JSON(c, http.StatusOK, res)
}

// Adjust godoc
//
//	@Summary	adjust inventory stock
//	@Description	Admin-only stock delta adjustment for one product.
//	@Tags		inventory
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		product_id	path		string					true	"Product ID"
//	@Param		_			body		dto.AdjustInventoryReq	true	"Body"
//	@Success	200			{object}	dto.Inventory
//	@Failure	403			{object}	response.Response	"Permission denied"
//	@Router		/api/v1/inventory/{product_id}/adjust [patch]
func (h *InventoryHandler) Adjust(c *gin.Context) {
	productID := c.Param("product_id")
	var req dto.AdjustInventoryReq
	if err := c.ShouldBindJSON(&req); c.Request.Body == nil || err != nil {
		logger.Error("Failed to parse body", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	item, err := h.service.AdjustStock(c, productID, &req)
	if err != nil {
		logger.Error("Failed to adjust inventory", err)
		status := http.StatusInternalServerError
		message := "Something went wrong"
		if errors.Is(err, service.ErrInsufficientStock) {
			status = http.StatusBadRequest
			message = err.Error()
		}
		response.Error(c, status, err, message)
		return
	}

	var res dto.Inventory
	utils.Copy(&res, &item)
	response.JSON(c, http.StatusOK, res)
}
