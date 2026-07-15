package http

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"

	"goshop/internal/product/dto"
	"goshop/internal/product/service"
	"goshop/pkg/config"
	"goshop/pkg/money"
	"goshop/pkg/redis"
	"goshop/pkg/response"
	"goshop/pkg/utils"
)

const productListCacheVersionKey = "cache:products:version"

type ProductHandler struct {
	cache   redis.IRedis
	service service.IProductService
}

func NewProductHandler(
	cache redis.IRedis,
	service service.IProductService,
) *ProductHandler {
	return &ProductHandler{
		cache:   cache,
		service: service,
	}
}

// GetProductByID godoc
//
//	@Summary	Get product by id
//	@Tags		products
//	@Produce	json
//	@Param		id	path	string	true	"Product ID"
//	@Router		/api/v1/products/{id} [get]
func (p *ProductHandler) GetProductByID(c *gin.Context) {
	var res dto.Product
	productId := c.Param("id")
	cacheKey := productDetailCacheKey(productId)
	if p.cache != nil {
		if err := p.cache.Get(cacheKey, &res); err == nil {
			response.JSON(c, http.StatusOK, res)
			return
		}
	}

	product, err := p.service.GetProductByID(c, productId)
	if err != nil {
		logger.Error("Failed to get product detail: ", err)
		response.Error(c, http.StatusNotFound, err, "Not found")
		return
	}

	utils.Copy(&res, &product)
	response.JSON(c, http.StatusOK, res)
	if p.cache != nil {
		_ = p.cache.SetWithExpiration(cacheKey, res, config.ProductCachingTime)
	}
}

// ListProducts godoc
//
//	@Summary	Get list products
//	@Tags		products
//	@Produce	json
//	@Success	200	{object}	dto.ListProductRes
//	@Router		/api/v1/products [get]
func (p *ProductHandler) ListProducts(c *gin.Context) {
	var req dto.ListProductReq
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error("Failed to parse request query: ", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	var res dto.ListProductRes
	cacheKey := productListCacheKey(p.productListCacheVersion(), &req)
	if p.cache != nil {
		if err := p.cache.Get(cacheKey, &res); err == nil {
			response.JSON(c, http.StatusOK, res)
			return
		}
	}

	products, pagination, err := p.service.ListProducts(c, &req)
	if err != nil {
		logger.Error("Failed to get list products: ", err)
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	utils.Copy(&res.Products, &products)
	res.Pagination = pagination
	response.JSON(c, http.StatusOK, res)
	if p.cache != nil {
		_ = p.cache.SetWithExpiration(cacheKey, res, config.ProductCachingTime)
	}
}

// CreateProduct godoc
//
//	@Summary	create product
//	@Description	Admin-only product creation.
//	@Tags		products
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		_	body	dto.CreateProductReq	true	"Body"
//	@Failure	403	{object}	response.Response	"Permission denied"
//	@Router		/api/v1/products [post]
func (p *ProductHandler) CreateProduct(c *gin.Context) {
	var req dto.CreateProductReq
	if err := c.ShouldBindJSON(&req); c.Request.Body == nil || err != nil {
		logger.Error("Failed to get body", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	product, err := p.service.Create(c, &req)
	if err != nil {
		logger.Error("Failed to create product", err.Error())
		if errors.Is(err, money.ErrInvalidAmount) {
			response.Error(c, http.StatusBadRequest, err, "Invalid price")
			return
		}
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.Product
	utils.Copy(&res, &product)
	p.invalidateProductLists()
	response.JSON(c, http.StatusOK, res)
}

// UpdateProduct godoc
//
//	@Summary	update product
//	@Description	Admin-only product update.
//	@Tags		products
//	@Produce	json
//	@Security	ApiKeyAuth
//	@Param		id	path	string					true	"Product ID"
//	@Param		_	body	dto.UpdateProductReq	true	"Body"
//	@Failure	403	{object}	response.Response	"Permission denied"
//	@Router		/api/v1/products/{id} [put]
func (p *ProductHandler) UpdateProduct(c *gin.Context) {
	productId := c.Param("id")
	var req dto.UpdateProductReq
	if err := c.ShouldBindJSON(&req); c.Request.Body == nil || err != nil {
		logger.Error("Failed to get body", err)
		response.Error(c, http.StatusBadRequest, err, "Invalid parameters")
		return
	}

	product, err := p.service.Update(c, productId, &req)
	if err != nil {
		logger.Error("Failed to update product", err.Error())
		if errors.Is(err, money.ErrInvalidAmount) {
			response.Error(c, http.StatusBadRequest, err, "Invalid price")
			return
		}
		response.Error(c, http.StatusInternalServerError, err, "Something went wrong")
		return
	}

	var res dto.Product
	utils.Copy(&res, &product)
	p.invalidateProduct(productId)
	response.JSON(c, http.StatusOK, res)
}

func productDetailCacheKey(productID string) string {
	return "cache:product:" + productID
}

func productListCacheKey(version int64, req *dto.ListProductReq) string {
	query := url.Values{}
	if req != nil {
		query.Set("code", req.Code)
		query.Set("limit", strconv.FormatInt(req.Limit, 10))
		query.Set("name", req.Name)
		query.Set("order_by", req.OrderBy)
		query.Set("order_desc", strconv.FormatBool(req.OrderDesc))
		query.Set("page", strconv.FormatInt(req.Page, 10))
	}
	digest := sha256.Sum256([]byte(query.Encode()))
	return fmt.Sprintf("cache:products:v%d:%x", version, digest)
}

func (p *ProductHandler) productListCacheVersion() int64 {
	if p.cache == nil {
		return 0
	}

	var version int64
	if err := p.cache.Get(productListCacheVersionKey, &version); err != nil || version < 0 {
		return 0
	}
	return version
}

func (p *ProductHandler) invalidateProductLists() {
	if p.cache == nil {
		return
	}
	if _, err := p.cache.IncrementWithExpiration(productListCacheVersionKey, 0); err != nil {
		logger.Errorf("product_list_cache_invalidation_failed error=%s", err)
	}
}

func (p *ProductHandler) invalidateProduct(productID string) {
	if p.cache == nil {
		return
	}
	if err := p.cache.Remove(productDetailCacheKey(productID)); err != nil {
		logger.Errorf("product_detail_cache_invalidation_failed error=%s", err)
	}
	p.invalidateProductLists()
}
