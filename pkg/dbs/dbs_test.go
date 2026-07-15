package dbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureConnectionPoolAppliesAllSettings(t *testing.T) {
	pool := &recordingConnectionPool{}
	cfg := Config{
		MaxOpenConns:    40,
		MaxIdleConns:    8,
		ConnMaxLifetime: 10 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}

	err := configureConnectionPool(pool, cfg)

	require.NoError(t, err)
	assert.Equal(t, 40, pool.maxOpenConns)
	assert.Equal(t, 8, pool.maxIdleConns)
	assert.Equal(t, 10*time.Minute, pool.connMaxLifetime)
	assert.Equal(t, 2*time.Minute, pool.connMaxIdleTime)
}

func TestConfigureConnectionPoolRejectsInvalidSettings(t *testing.T) {
	pool := &recordingConnectionPool{}

	err := configureConnectionPool(pool, Config{
		MaxOpenConns:    10,
		MaxIdleConns:    20,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})

	assert.Error(t, err)
	assert.Zero(t, pool.maxOpenConns)
}

func TestValidateMigrationStateAcceptsCompatibleFutureVersion(t *testing.T) {
	assert.NoError(t, validateMigrationState(5, false, 4))
}

func TestValidateMigrationStateRejectsOldOrDirtyVersion(t *testing.T) {
	assert.Error(t, validateMigrationState(3, false, 4))
	assert.Error(t, validateMigrationState(4, true, 4))
}

func TestGORMLoggerDoesNotExposeQueryParameters(t *testing.T) {
	filter, ok := newGORMLogger().(interface {
		ParamsFilter(context.Context, string, ...interface{}) (string, []interface{})
	})
	require.True(t, ok)

	query, params := filter.ParamsFilter(
		context.Background(),
		"SELECT * FROM users WHERE email = ? AND password = ?",
		"sentinel-email@example.com",
		"sentinel-password-hash",
	)

	assert.Equal(t, "SELECT * FROM users WHERE email = ? AND password = ?", query)
	assert.Empty(t, params)
}

func TestWithTransactionPassesTransactionHandleAndHonorsCommitAndRollback(t *testing.T) {
	databaseURI := os.Getenv("database_uri")
	if databaseURI == "" {
		databaseURI = "postgres://postgres:postgres@localhost:5432/goshop_test?sslmode=disable"
	}
	if !strings.Contains(databaseURI, "goshop_test") {
		t.Skip("transaction integration test requires a dedicated goshop_test database")
	}
	database, err := NewDatabase(Config{
		URI:             databaseURI,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: time.Minute,
	})
	if err != nil {
		t.Skipf("PostgreSQL integration service is unavailable: %v", err)
	}
	sqlDB, err := database.GetDB().DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	table := "transaction_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, database.GetDB().WithContext(ctx).Exec(fmt.Sprintf("CREATE TABLE %s (value TEXT NOT NULL)", table)).Error)
	t.Cleanup(func() {
		_ = database.GetDB().Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error
	})

	rollbackErr := errors.New("rollback requested")
	err = database.WithTransactionContext(ctx, func(tx IDatabase) error {
		assert.NotSame(t, database.GetDB(), tx.GetDB())
		require.NoError(t, tx.GetDB().WithContext(ctx).Exec(fmt.Sprintf("INSERT INTO %s (value) VALUES (?)", table), "rolled-back").Error)
		return rollbackErr
	})
	assert.ErrorIs(t, err, rollbackErr)
	assert.Equal(t, int64(0), countTransactionProbeRows(t, ctx, database, table))

	require.NoError(t, database.WithTransaction(func(tx IDatabase) error {
		return tx.GetDB().WithContext(ctx).Exec(fmt.Sprintf("INSERT INTO %s (value) VALUES (?)", table), "committed").Error
	}))
	assert.Equal(t, int64(1), countTransactionProbeRows(t, ctx, database, table))
}

func countTransactionProbeRows(t *testing.T, ctx context.Context, database IDatabase, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, database.GetDB().WithContext(ctx).Table(table).Count(&count).Error)
	return count
}

type recordingConnectionPool struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

func (p *recordingConnectionPool) SetMaxOpenConns(value int) {
	p.maxOpenConns = value
}

func (p *recordingConnectionPool) SetMaxIdleConns(value int) {
	p.maxIdleConns = value
}

func (p *recordingConnectionPool) SetConnMaxLifetime(value time.Duration) {
	p.connMaxLifetime = value
}

func (p *recordingConnectionPool) SetConnMaxIdleTime(value time.Duration) {
	p.connMaxIdleTime = value
}
