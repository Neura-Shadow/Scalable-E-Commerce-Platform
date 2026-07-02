package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"gorm.io/gorm"

	inventoryModel "goshop/internal/inventory/model"
	orderModel "goshop/internal/order/model"
	outboxModel "goshop/internal/outbox/model"
	productModel "goshop/internal/product/model"
	httpServer "goshop/internal/server/http"
	"goshop/internal/user/dto"
	userModel "goshop/internal/user/model"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
	"goshop/pkg/jtoken"
	"goshop/pkg/redis"
	"goshop/pkg/utils"
)

var (
	testRouter *gin.Engine
	dbTest     dbs.IDatabase
	testCache  redis.IRedis
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	setup()
	exitCode := m.Run()
	teardown()

	os.Exit(exitCode)
}

func setup() {
	cfg := config.LoadConfig()
	logger.Initialize(config.ProductionEnv)
	if !strings.Contains(cfg.DatabaseURI, "goshop_test") {
		logger.Fatal("HTTP integration tests require a dedicated goshop_test database")
	}

	var err error
	dbTest, err = dbs.NewDatabase(cfg.DatabaseURI)
	if err != nil {
		logger.Fatal("Cannot connect to database", err)
	}

	migrator := dbTest.GetDB().Migrator()
	_ = migrator.DropTable(&outboxModel.OutboxEvent{}, &orderModel.OrderLine{}, &orderModel.Order{}, &inventoryModel.Inventory{}, &productModel.Product{}, &userModel.User{})
	err = dbTest.AutoMigrate(&userModel.User{}, &productModel.Product{}, &inventoryModel.Inventory{}, orderModel.Order{}, orderModel.OrderLine{}, &outboxModel.OutboxEvent{})
	if err != nil {
		logger.Fatal("Database migration fail", err)
	}
	registerProductInventoryFixture()

	validator := validation.New()
	testCache = redis.New(redis.Config{
		Address:  cfg.RedisURI,
		Password: cfg.RedisPassword,
		Database: cfg.RedisDB,
	})

	server := httpServer.NewServer(validator, dbTest, testCache)
	_ = server.MapRoutes()
	testRouter = server.GetEngine()

	dbTest.Create(context.Background(), &userModel.User{
		Email:    "test@test.com",
		Password: "test123456",
		Role:     userModel.UserRoleAdmin,
	})
}

func teardown() {
	migrator := dbTest.GetDB().Migrator()
	migrator.DropTable(&userModel.User{}, &productModel.Product{}, &inventoryModel.Inventory{}, &orderModel.Order{}, &orderModel.OrderLine{}, &outboxModel.OutboxEvent{})
}

func registerProductInventoryFixture() {
	_ = dbTest.GetDB().Callback().Create().After("gorm:create").Register("test:create_inventory_for_product", func(tx *gorm.DB) {
		product, ok := tx.Statement.Dest.(*productModel.Product)
		if !ok || product.ID == "" {
			return
		}

		_ = tx.Session(&gorm.Session{NewDB: true}).Create(&inventoryModel.Inventory{
			ProductID: product.ID,
			Quantity:  1000000,
		}).Error
	})
}

func makeRequest(method, url string, body interface{}, token string) *httptest.ResponseRecorder {
	return makeRequestWithHeaders(method, url, body, token, nil)
}

func makeRequestWithHeaders(method, url string, body interface{}, token string, headers map[string]string) *httptest.ResponseRecorder {
	requestBody, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, url, bytes.NewBuffer(requestBody))
	if token != "" {
		request.Header.Add("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Add(key, value)
	}
	writer := httptest.NewRecorder()
	testRouter.ServeHTTP(writer, request)
	return writer
}

func accessToken() string {
	user := dto.LoginReq{
		Email:    "test@test.com",
		Password: "test123456",
	}

	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	return response["result"]["access_token"]
}

func refreshToken() string {
	user := dto.LoginReq{
		Email:    "test@test.com",
		Password: "test123456",
	}

	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	return response["result"]["refresh_token"]
}

func tokenForUser(user *userModel.User) string {
	return jtoken.GenerateAccessToken(map[string]interface{}{
		"id":    user.ID,
		"email": user.Email,
		"role":  user.Role,
	})
}

func parseResponseResult(resData []byte, result interface{}) {
	var response map[string]interface{}
	_ = json.Unmarshal(resData, &response)
	utils.Copy(result, response["result"])
}

func cleanData(records ...interface{}) {
	dbTest.GetDB().Where("1 = 1").Delete(&outboxModel.OutboxEvent{})
	dbTest.GetDB().Where("1 = 1").Delete(&orderModel.OrderLine{})
	dbTest.GetDB().Where("1 = 1").Delete(&inventoryModel.Inventory{})
	dbTest.GetDB().Where("1 = 1").Delete(&productModel.Product{})
	dbTest.GetDB().Where("1 = 1").Delete(&orderModel.Order{})

	for _, record := range records {
		dbTest.Delete(context.Background(), record)
	}

	cleanRedisTestData()
}

func cleanRedisTestData() {
	for _, pattern := range []string{
		"/api/v1/products*",
		"idempotency:orders:*",
		"rate-limit:orders:*",
		"processed:events:*",
		"consumer:failures:*",
		"stream:orders*",
	} {
		testCache.RemovePattern(pattern)
	}
}
