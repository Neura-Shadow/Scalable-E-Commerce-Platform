package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cartRepository "goshop/internal/cart/repository"
	inventoryModel "goshop/internal/inventory/model"
	orderModel "goshop/internal/order/model"
	outboxModel "goshop/internal/outbox/model"
	productModel "goshop/internal/product/model"
	userModel "goshop/internal/user/model"
	"goshop/pkg/dbs"
)

func TestInitialMigrationAppliesValidatesAndRollsBack(t *testing.T) {
	targetURI := createTemporaryDatabase(t)
	migrateBinary := migrationBinary(t)

	runMigration(t, migrateBinary, targetURI, "up")
	version := runMigration(t, migrateBinary, targetURI, "version")
	assert.Contains(t, version, "5")
	assert.Contains(t, runMigration(t, migrateBinary, targetURI, "up"), "no change")

	database, err := sql.Open("pgx", targetURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, database.PingContext(ctx))

	assertTablesExist(t, ctx, database, []string{
		"users",
		"products",
		"inventories",
		"orders",
		"order_lines",
		"outbox_events",
		"carts",
		"cart_lines",
	})
	assertIndexesExist(t, ctx, database, []string{
		"idx_inventory_product",
		"idx_orders_user_status_created_at",
		"idx_orders_user_idempotency_key",
		"idx_order_lines_order_id",
		"idx_outbox_events_status_next_attempt_at",
		"idx_outbox_events_processing_locked_at",
		"idx_cart_user",
		"idx_cart_lines_product_id",
	})
	assertConstraintsExist(t, ctx, database, []string{
		"inventories_quantity_check",
		"orders_status_check",
		"orders_idempotency_pair_check",
		"orders_total_price_scale_check",
		"outbox_events_status_check",
		"cart_lines_quantity_check",
		"users_token_version_check",
	})

	_, err = database.ExecContext(ctx, `
		INSERT INTO outbox_events (
			id, aggregate_type, aggregate_id, event_type, payload, status, attempts, next_attempt_at
		) VALUES ('invalid-status', 'order', 'order-1', 'order.created', '{}', 'invalid', 0, now())
	`)
	assert.Error(t, err, "the database must reject unknown outbox statuses")

	_, err = database.ExecContext(ctx, `
		INSERT INTO products (id, code, name, description, price, active)
		VALUES ('invalid-scale-product', 'INVALID-SCALE', 'Invalid Scale', 'fixture', 1.001, true)
	`)
	assert.Error(t, err, "the database must reject product prices with more than two decimal places")

	_, err = database.ExecContext(ctx, `
		INSERT INTO users (id, email, password, role)
		VALUES ('idempotency-user', 'idempotency@example.com', 'hash', 'customer')
	`)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO orders (
			id, code, user_id, total_price, status, idempotency_key, idempotency_fingerprint
		) VALUES (
			'idempotency-order-1', 'IDEMPOTENCY-ORDER-1', 'idempotency-user', 1.00, 'new',
			repeat('a', 64), repeat('b', 64)
		)
	`)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO orders (
			id, code, user_id, total_price, status, idempotency_key, idempotency_fingerprint
		) VALUES (
			'idempotency-order-2', 'IDEMPOTENCY-ORDER-2', 'idempotency-user', 1.00, 'new',
			repeat('a', 64), repeat('c', 64)
		)
	`)
	assert.Error(t, err, "the database must reject duplicate user idempotency keys")

	runMigration(t, migrateBinary, targetURI, "down", "-all")
	assertTableMissing(t, ctx, database, "users")
	assert.Contains(t, runMigration(t, migrateBinary, targetURI, "down", "-all"), "no change")
}

func TestInitialMigrationAdoptsLegacyAutoMigrateSchema(t *testing.T) {
	freshURI := createTemporaryDatabase(t)
	runMigration(t, migrationBinary(t), freshURI, "up")

	targetURI := createTemporaryDatabase(t)
	legacy, err := dbs.NewDatabase(dbs.Config{
		URI:             targetURI,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, legacy.AutoMigrate(
		&userModel.User{},
		&productModel.Product{},
		&inventoryModel.Inventory{},
		orderModel.Order{},
		orderModel.OrderLine{},
		&outboxModel.OutboxEvent{},
	))
	legacySQL, err := legacy.GetDB().DB()
	require.NoError(t, err)
	require.NoError(t, legacySQL.Close())

	migrateBinary := migrationBinary(t)
	runMigration(t, migrateBinary, targetURI, "up")
	assert.Contains(t, runMigration(t, migrateBinary, targetURI, "version"), "5")

	database, err := sql.Open("pgx", targetURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assertTablesExist(t, ctx, database, []string{"carts", "cart_lines"})
	assertConstraintsExist(t, ctx, database, []string{
		"users_role_check",
		"products_price_check",
		"inventories_quantity_check",
		"orders_status_check",
		"outbox_events_status_check",
		"cart_lines_quantity_check",
		"users_token_version_check",
	})
	assertColumnSchemasEqual(t, freshURI, targetURI)
}

func TestLegacyAdoptionPreflightRejectsDuplicateDataBeforeV1Changes(t *testing.T) {
	targetURI := createTemporaryDatabase(t)
	legacy, err := dbs.NewDatabase(dbs.Config{
		URI:             targetURI,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, legacy.AutoMigrate(
		&userModel.User{},
		&productModel.Product{},
		&inventoryModel.Inventory{},
		orderModel.Order{},
		orderModel.OrderLine{},
		&outboxModel.OutboxEvent{},
	))
	require.NoError(t, legacy.GetDB().Exec("DROP INDEX IF EXISTS idx_product_name").Error)
	require.NoError(t, legacy.GetDB().Exec(`
		INSERT INTO products (id, code, name, description, price, active)
		VALUES
			('legacy-duplicate-1', 'LEGACY-DUP-1', 'Duplicate Name', 'fixture', 1, true),
			('legacy-duplicate-2', 'LEGACY-DUP-2', 'Duplicate Name', 'fixture', 1, true)
	`).Error)
	legacySQL, err := legacy.GetDB().DB()
	require.NoError(t, err)
	require.NoError(t, legacySQL.Close())

	migrateBinary := migrationBinary(t)
	output := runMigrationExpectFailure(t, migrateBinary, targetURI, "up")
	assert.Contains(t, output, "products because name contains duplicate values")

	database, err := sql.Open("pgx", targetURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var version uint
	var dirty bool
	require.NoError(t, database.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, uint(1), version)
	assert.True(t, dirty)

	var indexExists bool
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = 'idx_orders_user_status_created_at'
		)
	`).Scan(&indexExists))
	assert.False(t, indexExists, "v1 preflight must run before creating indexes")

	_, err = database.ExecContext(ctx, `
		UPDATE products SET name = 'Repaired Duplicate Name' WHERE id = 'legacy-duplicate-2'
	`)
	require.NoError(t, err)
	runMigration(t, migrateBinary, targetURI, "force", "--", "-1")
	runMigration(t, migrateBinary, targetURI, "up")
	require.NoError(t, database.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, uint(5), version)
	assert.False(t, dirty)
}

func TestLegacySchemaHardeningRejectsAmbiguousNullBusinessData(t *testing.T) {
	targetURI := createTemporaryDatabase(t)
	legacy, err := dbs.NewDatabase(dbs.Config{
		URI:             targetURI,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, legacy.AutoMigrate(
		&userModel.User{},
		&productModel.Product{},
		&inventoryModel.Inventory{},
		orderModel.Order{},
		orderModel.OrderLine{},
		&outboxModel.OutboxEvent{},
	))
	require.NoError(t, legacy.GetDB().Exec(`
		INSERT INTO products (id, code, name, description, price, active)
		VALUES ('legacy-invalid-product', 'LEGACY-INVALID', NULL, 'requires repair', 1, true)
	`).Error)
	legacySQL, err := legacy.GetDB().DB()
	require.NoError(t, err)
	require.NoError(t, legacySQL.Close())

	output := runMigrationExpectFailure(t, migrationBinary(t), targetURI, "up")
	assert.Contains(t, output, "cannot harden products.name")

	database, err := sql.Open("pgx", targetURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var version uint
	var dirty bool
	require.NoError(t, database.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, uint(4), version)
	assert.True(t, dirty)

	var createdAt sql.NullTime
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT created_at FROM products WHERE id = 'legacy-invalid-product'
	`).Scan(&createdAt))
	assert.False(t, createdAt.Valid, "ambiguous-data preflight must run before safe-default backfills")

	_, err = database.ExecContext(ctx, `
		UPDATE products SET name = 'Repaired Legacy Product' WHERE id = 'legacy-invalid-product'
	`)
	require.NoError(t, err)

	migrateBinary := migrationBinary(t)
	runMigration(t, migrateBinary, targetURI, "force", "3")
	runMigration(t, migrateBinary, targetURI, "up")
	require.NoError(t, database.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, uint(5), version)
	assert.False(t, dirty)
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT created_at FROM products WHERE id = 'legacy-invalid-product'
	`).Scan(&createdAt))
	assert.True(t, createdAt.Valid, "safe defaults are backfilled after ambiguous data is repaired")
}

func TestCartRepositoryAtomicOperationsAfterMigrations(t *testing.T) {
	targetURI := createTemporaryDatabase(t)
	runMigration(t, migrationBinary(t), targetURI, "up")

	database, err := dbs.NewDatabase(dbs.Config{
		URI:             targetURI,
		MaxOpenConns:    32,
		MaxIdleConns:    16,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	require.NoError(t, err)
	sqlDB, err := database.GetDB().DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, database.GetDB().WithContext(ctx).Exec(
		"INSERT INTO users (id, email, password, role) VALUES (?, ?, ?, ?)",
		"cart-user",
		"cart@example.com",
		"hash",
		"customer",
	).Error)

	const productCount = 16
	productIDs := make([]string, 0, productCount)
	for i := 0; i < productCount; i++ {
		productID := fmt.Sprintf("cart-product-%02d", i)
		productIDs = append(productIDs, productID)
		require.NoError(t, database.GetDB().WithContext(ctx).Exec(
			"INSERT INTO products (id, code, name, description, price, active) VALUES (?, ?, ?, ?, ?, ?)",
			productID,
			fmt.Sprintf("CP%02d", i),
			fmt.Sprintf("Cart Product %02d", i),
			"concurrency fixture",
			10+i,
			true,
		).Error)
	}

	repository := cartRepository.NewCartRepository(database)
	start := make(chan struct{})
	errorsByProduct := make(chan error, productCount)
	var workers sync.WaitGroup
	for _, productID := range productIDs {
		workers.Add(1)
		go func(productID string) {
			defer workers.Done()
			<-start
			_, addErr := repository.AddProduct(ctx, "cart-user", productID, 1)
			errorsByProduct <- addErr
		}(productID)
	}
	close(start)
	workers.Wait()
	close(errorsByProduct)
	for addErr := range errorsByProduct {
		require.NoError(t, addErr)
	}

	loaded, err := repository.GetCartByUserID(ctx, "cart-user")
	require.NoError(t, err)
	require.Len(t, loaded.Lines, productCount)
	uniqueProducts := make(map[string]struct{}, productCount)
	for _, line := range loaded.Lines {
		uniqueProducts[line.ProductID] = struct{}{}
		assert.Equal(t, uint(1), line.Quantity)
		assert.Equal(t, loaded.ID, line.CartID)
	}
	assert.Len(t, uniqueProducts, productCount)

	var cartCount int64
	require.NoError(t, database.GetDB().WithContext(ctx).Table("carts").Where("user_id = ?", "cart-user").Count(&cartCount).Error)
	assert.Equal(t, int64(1), cartCount)

	duplicate, err := repository.AddProduct(ctx, "cart-user", productIDs[0], 99)
	require.NoError(t, err)
	require.Len(t, duplicate.Lines, productCount)
	for _, line := range duplicate.Lines {
		if line.ProductID == productIDs[0] {
			assert.Equal(t, uint(1), line.Quantity, "adding an existing product remains idempotent")
		}
	}

	updated, err := repository.RemoveProduct(ctx, "cart-user", productIDs[0])
	require.NoError(t, err)
	require.Len(t, updated.Lines, productCount-1)
	for _, line := range updated.Lines {
		assert.NotEqual(t, productIDs[0], line.ProductID)
	}
}

func migrationBinary(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("MIGRATE_BIN"); configured != "" {
		return configured
	}
	if binary, err := exec.LookPath("migrate"); err == nil {
		return binary
	}

	goPath, err := exec.Command("go", "env", "GOPATH").Output()
	require.NoError(t, err)
	binaryName := "migrate"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(strings.TrimSpace(string(goPath)), "bin", binaryName)
	_, err = os.Stat(binary)
	require.NoError(t, err, "install the pinned migration CLI with: go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3")
	return binary
}

func runMigration(t *testing.T, binary, databaseURI string, arguments ...string) string {
	t.Helper()
	args := []string{"-source", migrationSourceURL(t), "-database", databaseURI}
	args = append(args, arguments...)
	command := exec.Command(binary, args...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "migration command failed: %s", strings.TrimSpace(string(output)))
	return strings.ToLower(strings.TrimSpace(string(output)))
}

func runMigrationExpectFailure(t *testing.T, binary, databaseURI string, arguments ...string) string {
	t.Helper()
	args := []string{"-source", migrationSourceURL(t), "-database", databaseURI}
	args = append(args, arguments...)
	command := exec.Command(binary, args...)
	output, err := command.CombinedOutput()
	require.Error(t, err, "migration command unexpectedly succeeded: %s", strings.TrimSpace(string(output)))
	return strings.ToLower(strings.TrimSpace(string(output)))
}

func createTemporaryDatabase(t *testing.T) string {
	t.Helper()

	baseURI := os.Getenv("database_uri")
	if baseURI == "" {
		baseURI = "postgres://postgres:postgres@localhost:5432/goshop_test?sslmode=disable"
	}
	parsed, err := url.Parse(baseURI)
	require.NoError(t, err)
	databaseName := "goshop_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	adminURI := *parsed
	adminURI.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURI.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("PostgreSQL integration service is unavailable: %v", err)
	}

	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+databaseName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, databaseName)
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+databaseName)
	})

	targetURI := *parsed
	targetURI.Path = "/" + databaseName
	return targetURI.String()
}

func migrationSourceURL(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	return (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(filepath.Join(root, "migrations")),
	}).String()
}

func assertTablesExist(t *testing.T, ctx context.Context, database *sql.DB, tables []string) {
	t.Helper()
	for _, table := range tables {
		var name sql.NullString
		require.NoError(t, database.QueryRowContext(ctx, "SELECT to_regclass($1)", "public."+table).Scan(&name))
		assert.True(t, name.Valid, fmt.Sprintf("table %s must exist", table))
	}
}

func assertIndexesExist(t *testing.T, ctx context.Context, database *sql.DB, indexes []string) {
	t.Helper()
	for _, index := range indexes {
		var exists bool
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1
			)
		`, index).Scan(&exists))
		assert.True(t, exists, fmt.Sprintf("index %s must exist", index))
	}
}

func assertConstraintsExist(t *testing.T, ctx context.Context, database *sql.DB, constraints []string) {
	t.Helper()
	for _, constraint := range constraints {
		var exists bool
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = $1
			)
		`, constraint).Scan(&exists))
		assert.True(t, exists, fmt.Sprintf("constraint %s must exist", constraint))
	}
}

func assertTableMissing(t *testing.T, ctx context.Context, database *sql.DB, table string) {
	t.Helper()
	var name sql.NullString
	require.NoError(t, database.QueryRowContext(ctx, "SELECT to_regclass($1)", "public."+table).Scan(&name))
	assert.False(t, name.Valid)
}

type columnSchema struct {
	TableName     string
	ColumnName    string
	DataType      string
	UDTName       string
	Nullable      string
	ColumnDefault string
}

func assertColumnSchemasEqual(t *testing.T, freshURI, adoptedURI string) {
	t.Helper()
	fresh := readColumnSchemas(t, freshURI)
	adopted := readColumnSchemas(t, adoptedURI)
	assert.Equal(t, fresh, adopted, "fresh and adopted databases must expose identical application column contracts")
}

func readColumnSchemas(t *testing.T, databaseURI string) []columnSchema {
	t.Helper()
	database, err := sql.Open("pgx", databaseURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
		SELECT
			table_name,
			column_name,
			data_type,
			udt_name,
			is_nullable,
			COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN (
			'users', 'products', 'inventories', 'orders',
			'order_lines', 'outbox_events', 'carts', 'cart_lines'
		  )
		ORDER BY table_name, ordinal_position
	`)
	require.NoError(t, err)
	defer rows.Close()

	var schemas []columnSchema
	for rows.Next() {
		var schema columnSchema
		require.NoError(t, rows.Scan(
			&schema.TableName,
			&schema.ColumnName,
			&schema.DataType,
			&schema.UDTName,
			&schema.Nullable,
			&schema.ColumnDefault,
		))
		schemas = append(schemas, schema)
	}
	require.NoError(t, rows.Err())
	return schemas
}
