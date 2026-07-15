package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"goshop/internal/product/dto"
	"goshop/internal/product/model"
	srvMocks "goshop/internal/product/service/mocks"
	"goshop/pkg/config"
	"goshop/pkg/money"
	"goshop/pkg/paging"
	redisMocks "goshop/pkg/redis/mocks"
	"goshop/pkg/response"
	"goshop/pkg/utils"
)

type ProductHandlerTestSuite struct {
	suite.Suite
	mockService *srvMocks.IProductService
	mockRedis   *redisMocks.IRedis
	handler     *ProductHandler
}

func (suite *ProductHandlerTestSuite) SetupTest() {
	logger.Initialize(config.ProductionEnv)

	suite.mockService = srvMocks.NewIProductService(suite.T())
	suite.mockRedis = redisMocks.NewIRedis(suite.T())
	suite.handler = NewProductHandler(suite.mockRedis, suite.mockService)
}

func TestProductHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(ProductHandlerTestSuite))
}

func TestProductListCacheKeyNormalizesEquivalentQueries(t *testing.T) {
	first := &dto.ListProductReq{
		Name:      "keyboard",
		Page:      2,
		Limit:     25,
		OrderBy:   "price",
		OrderDesc: true,
	}
	second := &dto.ListProductReq{
		OrderDesc: true,
		Limit:     25,
		Name:      "keyboard",
		OrderBy:   "price",
		Page:      2,
	}

	assert.Equal(t, productListCacheKey(7, first), productListCacheKey(7, second))
}

func TestProductListCacheKeySeparatesVersionsFiltersAndPages(t *testing.T) {
	base := &dto.ListProductReq{Name: "keyboard", Page: 1, Limit: 20}
	differentFilter := &dto.ListProductReq{Name: "monitor", Page: 1, Limit: 20}
	differentPage := &dto.ListProductReq{Name: "keyboard", Page: 2, Limit: 20}

	assert.NotEqual(t, productListCacheKey(1, base), productListCacheKey(1, differentFilter))
	assert.NotEqual(t, productListCacheKey(1, base), productListCacheKey(1, differentPage))
	assert.NotEqual(t, productListCacheKey(1, base), productListCacheKey(2, base))
}

func TestProductDetailCacheKeyUsesExactProductID(t *testing.T) {
	assert.Equal(t, "cache:product:product-123", productDetailCacheKey("product-123"))
}

func (suite *ProductHandlerTestSuite) prepareContext(path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	requestBody, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", path, bytes.NewBuffer(requestBody))
	c, _ := gin.CreateTestContext(w)
	c.Request = r

	return c, w
}

func (suite *ProductHandlerTestSuite) expectProductListVersion(version int64, err error) {
	suite.mockRedis.
		On("Get", productListCacheVersionKey, mock.AnythingOfType("*int64")).
		Run(func(args mock.Arguments) {
			value := args.Get(1).(*int64)
			*value = version
		}).
		Return(err).
		Times(1)
}

// GetProductByID
// =================================================================================================

func (suite *ProductHandlerTestSuite) TestGetProductByIDSuccessfullyFromDatabase() {
	ctx, writer := suite.prepareContext("/api/v1/products/123456", nil)
	ctx.Params = []gin.Param{{Key: "id", Value: "123456"}}

	suite.mockRedis.On("Get", productDetailCacheKey("123456"), &dto.Product{}).Return(errors.New("not found")).Times(1)
	suite.mockService.On("GetProductByID", mock.Anything, mock.Anything).
		Return(
			&model.Product{
				ID:          "123456",
				Name:        "product",
				Description: "description",
			},
			nil,
		).Times(1)
	suite.mockRedis.On("SetWithExpiration", mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)

	suite.handler.GetProductByID(ctx)

	var res response.Response
	var product dto.Product

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&product, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal("123456", product.ID)
	suite.Equal("product", product.Name)
	suite.Equal("description", product.Description)
}

func (suite *ProductHandlerTestSuite) TestGetProductByIDSuccessfullyFromCache() {
	ctx, writer := suite.prepareContext("/api/v1/products/123456", nil)
	ctx.Params = []gin.Param{{Key: "id", Value: "123456"}}

	suite.mockRedis.On("Get", productDetailCacheKey("123456"), &dto.Product{}).Return(nil).Times(1)

	suite.handler.GetProductByID(ctx)

	var res response.Response
	var product dto.Product

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&product, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal("", product.ID)
	suite.Equal("", product.Name)
	suite.Equal("", product.Description)
}

func (suite *ProductHandlerTestSuite) TestGetProductByIDFail() {
	ctx, writer := suite.prepareContext("/api/v1/products/123456", nil)
	ctx.Params = []gin.Param{{Key: "id", Value: "123456"}}

	suite.mockRedis.On("Get", productDetailCacheKey("123456"), &dto.Product{}).Return(errors.New("not found")).Times(1)
	suite.mockService.On("GetProductByID", mock.Anything, mock.Anything).
		Return(nil, errors.New("error")).Times(1)

	suite.handler.GetProductByID(ctx)
	suite.Equal(http.StatusNotFound, writer.Code)
}

// ListProducts
// =================================================================================================

func (suite *ProductHandlerTestSuite) TestListProductsSuccessfullyFromDatabase() {
	ctx, writer := suite.prepareContext("/api/v1/products", nil)

	suite.expectProductListVersion(0, errors.New("not found"))
	suite.mockRedis.On("Get", productListCacheKey(0, &dto.ListProductReq{}), &dto.ListProductRes{}).Return(errors.New("not found")).Times(1)
	suite.mockService.On("ListProducts", mock.Anything, mock.Anything).
		Return(
			[]*model.Product{
				{
					ID:          "123456",
					Name:        "product",
					Description: "description",
				},
			},
			&paging.Pagination{},
			nil,
		).Times(1)
	suite.mockRedis.On("SetWithExpiration", mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)

	suite.handler.ListProducts(ctx)

	var res response.Response
	var products dto.ListProductRes

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&products, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(1, len(products.Products))
	suite.Equal("123456", products.Products[0].ID)
	suite.Equal("product", products.Products[0].Name)
	suite.Equal("description", products.Products[0].Description)
}

func (suite *ProductHandlerTestSuite) TestListProductsSuccessfullyFromCache() {
	ctx, writer := suite.prepareContext("/api/v1/products", nil)

	suite.expectProductListVersion(7, nil)
	suite.mockRedis.On("Get", productListCacheKey(7, &dto.ListProductReq{}), &dto.ListProductRes{}).Return(nil).Times(1)

	suite.handler.ListProducts(ctx)

	var res response.Response
	var products []*dto.Product

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&products, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(0, len(products))
}

func (suite *ProductHandlerTestSuite) TestListProductsInvalidQuery() {
	ctx, writer := suite.prepareContext("/api/v1/products?page=a", nil)
	suite.handler.ListProducts(ctx)
	suite.Equal(http.StatusBadRequest, writer.Code)
}

func (suite *ProductHandlerTestSuite) TestListProductsFail() {
	ctx, writer := suite.prepareContext("/api/v1/products", nil)

	suite.expectProductListVersion(0, errors.New("not found"))
	suite.mockRedis.On("Get", productListCacheKey(0, &dto.ListProductReq{}), &dto.ListProductRes{}).Return(errors.New("not found")).Times(1)
	suite.mockService.On("ListProducts", mock.Anything, mock.Anything).
		Return(nil, nil, errors.New("error")).Times(1)

	suite.handler.ListProducts(ctx)
	suite.Equal(http.StatusInternalServerError, writer.Code)
}

// CreateProduct
// =================================================================================================
func (suite *ProductHandlerTestSuite) TestCreateProductSuccess() {
	req := &dto.CreateProductReq{
		Name:        "product",
		Description: "description",
		Price:       10.5,
	}

	ctx, writer := suite.prepareContext("/api/v1/products", req)

	suite.mockService.On("Create", mock.Anything, req).
		Return(
			&model.Product{
				Name:        "product",
				Description: "description",
				Price:       10.5,
			},
			nil,
		).Times(1)
	suite.mockRedis.On("IncrementWithExpiration", productListCacheVersionKey, time.Duration(0)).Return(int64(1), nil).Times(1)

	suite.handler.CreateProduct(ctx)

	var res response.Response
	var resData dto.Product

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&resData, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal(req.Name, resData.Name)
	suite.Equal(req.Description, resData.Description)
	suite.Equal(req.Price, resData.Price)
}

func (suite *ProductHandlerTestSuite) TestCreateProductInvalidPriceType() {
	req := map[string]any{
		"name":        "product",
		"description": "description",
		"price":       "10.5",
	}

	ctx, writer := suite.prepareContext("/api/v1/products", req)
	suite.handler.CreateProduct(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.Equal("Invalid parameters", res["error"]["message"])
}

func (suite *ProductHandlerTestSuite) TestCreateProductFail() {
	req := &dto.CreateProductReq{
		Name:        "product",
		Description: "description",
		Price:       10.5,
	}

	ctx, writer := suite.prepareContext("/api/v1/products", req)

	suite.mockService.On("Create", mock.Anything, req).
		Return(nil, errors.New("error")).Times(1)

	suite.handler.CreateProduct(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusInternalServerError, writer.Code)
	suite.Equal("Something went wrong", res["error"]["message"])
}

func (suite *ProductHandlerTestSuite) TestCreateProductRejectsInvalidPricePrecision() {
	req := &dto.CreateProductReq{Name: "product", Description: "description", Price: 1.001}
	ctx, writer := suite.prepareContext("/api/v1/products", req)
	suite.mockService.On("Create", mock.Anything, mock.Anything).Return(nil, money.ErrInvalidAmount).Once()

	suite.handler.CreateProduct(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.Equal("Invalid price", res["error"]["message"])
}

// UpdateProduct
// =================================================================================================

func (suite *ProductHandlerTestSuite) TestUpdateProductSuccess() {
	req := &dto.UpdateProductReq{
		Name:        "product",
		Description: "description",
		Price:       10.5,
	}

	ctx, writer := suite.prepareContext("/api/v1/products/123456", req)
	ctx.Params = []gin.Param{{Key: "id", Value: "123456"}}

	suite.mockService.On("Update", mock.Anything, mock.Anything, req).
		Return(
			&model.Product{
				ID:          "123456",
				Name:        "product",
				Description: "description",
				Price:       10.5,
			},
			nil,
		).Times(1)
	suite.mockRedis.On("Remove", productDetailCacheKey("123456")).Return(nil).Times(1)
	suite.mockRedis.On("IncrementWithExpiration", productListCacheVersionKey, time.Duration(0)).Return(int64(1), nil).Times(1)

	suite.handler.UpdateProduct(ctx)

	var res response.Response
	var resData dto.Product

	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	utils.Copy(&resData, &res.Result)
	suite.Equal(http.StatusOK, writer.Code)
	suite.Equal("123456", resData.ID)
	suite.Equal(req.Name, resData.Name)
	suite.Equal(req.Description, resData.Description)
	suite.Equal(req.Price, resData.Price)
}

func (suite *ProductHandlerTestSuite) TestUpdateProductInvalidPriceType() {
	req := map[string]any{
		"name":        "product",
		"description": "description",
		"price":       "10.5",
	}

	ctx, writer := suite.prepareContext("/api/v1/products/123456", req)
	suite.handler.UpdateProduct(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusBadRequest, writer.Code)
	suite.Equal("Invalid parameters", res["error"]["message"])
}

func (suite *ProductHandlerTestSuite) TestUpdateProductFail() {
	req := &dto.UpdateProductReq{
		Name:        "product",
		Description: "description",
		Price:       10.5,
	}

	ctx, writer := suite.prepareContext("/api/v1/products/123456", req)

	suite.mockService.On("Update", mock.Anything, mock.Anything, req).
		Return(nil, errors.New("error")).Times(1)

	suite.handler.UpdateProduct(ctx)

	var res map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &res)
	suite.Equal(http.StatusInternalServerError, writer.Code)
	suite.Equal("Something went wrong", res["error"]["message"])
}
