package repository

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inventoryModel "goshop/internal/inventory/model"
	productModel "goshop/internal/product/model"
	"goshop/pkg/dbs"
)

func TestInventoryListOrderUsesAllowedColumn(t *testing.T) {
	order := inventoryListOrder("quantity", true)

	assert.Equal(t, "quantity", order.Column.Name)
	assert.True(t, order.Desc)
}

func TestInventoryListOrderRejectsUnsafeColumn(t *testing.T) {
	order := inventoryListOrder("quantity DESC; DROP TABLE inventories;--", true)

	assert.Equal(t, "created_at", order.Column.Name)
	assert.False(t, order.Desc)
}

func TestAdjustStockAndConsumeStockRemainAtomicUnderConcurrency(t *testing.T) {
	databaseURI := os.Getenv("database_uri")
	if databaseURI == "" {
		databaseURI = "postgres://postgres:postgres@localhost:5432/goshop_test?sslmode=disable"
	}
	if !strings.Contains(databaseURI, "goshop_test") {
		t.Skip("inventory concurrency test requires a dedicated goshop_test database")
	}

	database, err := dbs.NewDatabase(dbs.Config{
		URI:             databaseURI,
		MaxOpenConns:    64,
		MaxIdleConns:    16,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	if err != nil {
		t.Skipf("PostgreSQL integration service is unavailable: %v", err)
	}
	sqlDB, err := database.GetDB().DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := uuid.NewString()
	product := productModel.Product{
		Name:        "atomic-inventory-" + suffix,
		Description: "inventory concurrency fixture",
		Price:       1,
	}
	require.NoError(t, database.GetDB().WithContext(ctx).Create(&product).Error)
	t.Cleanup(func() {
		_ = database.GetDB().Unscoped().Where("product_id = ?", product.ID).Delete(&inventoryModel.Inventory{}).Error
		_ = database.GetDB().Unscoped().Delete(&product).Error
	})

	const pairs = 32
	inventory := inventoryModel.Inventory{ProductID: product.ID, Quantity: pairs}
	require.NoError(t, database.GetDB().WithContext(ctx).Create(&inventory).Error)

	repository := NewInventoryRepository(database)
	start := make(chan struct{})
	errorsByWorker := make(chan error, pairs*2)
	var workers sync.WaitGroup
	for range pairs {
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			_, adjusted, err := repository.AdjustStock(ctx, product.ID, 1)
			if err == nil && !adjusted {
				err = assert.AnError
			}
			errorsByWorker <- err
		}()
		go func() {
			defer workers.Done()
			<-start
			consumed, err := repository.ConsumeStock(ctx, product.ID, 1)
			if err == nil && !consumed {
				err = assert.AnError
			}
			errorsByWorker <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		require.NoError(t, err)
	}

	updated, err := repository.GetByProductID(ctx, product.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(pairs), updated.Quantity)
}
