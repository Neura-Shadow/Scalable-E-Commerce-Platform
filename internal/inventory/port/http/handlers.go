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
