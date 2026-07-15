package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
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
	cleanupDB  func()
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
	testDatabaseURI, cleanup, err := createTemporaryTestDatabase(cfg.DatabaseURI)
	if err != nil {
		logger.Fatal("Cannot create temporary HTTP test database", err)
	}
	cleanupDB = cleanup
	if err = applyTestMigrations(testDatabaseURI); err != nil {
		cleanupDB()
		logger.Fatal("Cannot apply HTTP test migrations", err)
	}

	dbTest, err = dbs.NewDatabase(dbs.Config{
		URI:             testDatabaseURI,
		MaxOpenConns:    config.DatabaseMaxOpenConns(),
		MaxIdleConns:    config.DatabaseMaxIdleConns(),
		ConnMaxLifetime: config.DatabaseConnMaxLifetime(),
		ConnMaxIdleTime: config.DatabaseConnMaxIdleTime(),
	})
	if err != nil {
		logger.Fatal("Cannot connect to database", err)
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
	if dbTest != nil {
		if database, err := dbTest.GetDB().DB(); err == nil {
			_ = database.Close()
		}
	}
	if cleanupDB != nil {
		cleanupDB()
	}
}

func createTemporaryTestDatabase(baseURI string) (string, func(), error) {
	parsed, err := url.Parse(baseURI)
	if err != nil {
		return "", nil, err
	}
	databaseName := "goshop_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	adminURI := *parsed
	adminURI.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURI.String())
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		return "", nil, err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+databaseName); err != nil {
		_ = admin.Close()
		return "", nil, err
	}

	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = admin.ExecContext(cleanupCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, databaseName)
		_, _ = admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+databaseName)
		_ = admin.Close()
	}

	targetURI := *parsed
	targetURI.Path = "/" + databaseName
	return targetURI.String(), cleanup, nil
}

func applyTestMigrations(databaseURI string) error {
	binary := os.Getenv("MIGRATE_BIN")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("migrate")
		if err != nil {
			goPath, goPathErr := exec.Command("go", "env", "GOPATH").Output()
			if goPathErr != nil {
				return goPathErr
			}
			binaryName := "migrate"
			if runtime.GOOS == "windows" {
				binaryName += ".exe"
			}
			binary = filepath.Join(strings.TrimSpace(string(goPath)), "bin", binaryName)
			if _, statErr := os.Stat(binary); statErr != nil {
				return fmt.Errorf("migration CLI unavailable: %w", statErr)
			}
		}
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("cannot resolve HTTP test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	source := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, "migrations"))}).String()
	command := exec.Command(binary, "-source", source, "-database", databaseURI, "up")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("migration command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
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
		"cache:product:*",
		"cache:products:*",
		"idempotency:orders:*",
		"rate-limit:orders:*",
		"processed:events:*",
		"consumer:failures:*",
		"stream:orders*",
	} {
		testCache.RemovePattern(pattern)
	}
}
