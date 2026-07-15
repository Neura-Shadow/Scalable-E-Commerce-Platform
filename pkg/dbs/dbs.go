package dbs

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

const DatabaseTimeout = 5 * time.Second

//go:generate mockery --name=IDatabase
type IDatabase interface {
	GetDB() *gorm.DB
	Ping(ctx context.Context) error
	HasTables(ctx context.Context, tables []string) error
	MigrationReady(ctx context.Context, minimumVersion uint) error
	AutoMigrate(models ...any) error
	WithTransaction(function func(tx IDatabase) error) error
	WithTransactionContext(ctx context.Context, function func(tx IDatabase) error) error
	Create(ctx context.Context, doc any) error
	CreateInBatches(ctx context.Context, docs any, batchSize int) error
	Update(ctx context.Context, doc any) error
	Delete(ctx context.Context, value any, opts ...FindOption) error
	FindById(ctx context.Context, id string, result any) error
	FindOne(ctx context.Context, result any, opts ...FindOption) error
	Find(ctx context.Context, result any, opts ...FindOption) error
	Count(ctx context.Context, model any, total *int64, opts ...FindOption) error
}

type Query struct {
	Query string
	Args  []any
}

type Config struct {
	URI             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type connectionPool interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxLifetime(time.Duration)
	SetConnMaxIdleTime(time.Duration)
}

func NewQuery(query string, args ...any) Query {
	return Query{
		Query: query,
		Args:  args,
	}
}

type Database struct {
	db *gorm.DB
}

func NewDatabase(config Config) (*Database, error) {
	if config.URI == "" {
		return nil, fmt.Errorf("database URI is required")
	}
	if err := validateConnectionPoolConfig(config); err != nil {
		return nil, err
	}

	database, err := gorm.Open(postgres.Open(config.URI), &gorm.Config{
		Logger: newGORMLogger(),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	if err := configureConnectionPool(sqlDB, config); err != nil {
		return nil, err
	}

	return &Database{
		db: database,
	}, nil
}

func newGORMLogger() gormLogger.Interface {
	return gormLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormLogger.Config{
			SlowThreshold:        200 * time.Millisecond,
			LogLevel:             gormLogger.Warn,
			ParameterizedQueries: true,
			Colorful:             false,
		},
	)
}

func configureConnectionPool(pool connectionPool, config Config) error {
	if pool == nil {
		return fmt.Errorf("database connection pool is required")
	}
	if err := validateConnectionPoolConfig(config); err != nil {
		return err
	}

	pool.SetMaxOpenConns(config.MaxOpenConns)
	pool.SetMaxIdleConns(config.MaxIdleConns)
	pool.SetConnMaxLifetime(config.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(config.ConnMaxIdleTime)
	return nil
}

func validateConnectionPoolConfig(config Config) error {
	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("database max open connections must be greater than zero")
	}
	if config.MaxIdleConns <= 0 {
		return fmt.Errorf("database max idle connections must be greater than zero")
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("database max idle connections must not exceed max open connections")
	}
	if config.ConnMaxLifetime <= 0 {
		return fmt.Errorf("database connection max lifetime must be greater than zero")
	}
	if config.ConnMaxIdleTime <= 0 {
		return fmt.Errorf("database connection max idle time must be greater than zero")
	}
	return nil
}

func (d *Database) AutoMigrate(models ...any) error {
	return d.db.AutoMigrate(models...)
}

func (d *Database) Ping(ctx context.Context) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (d *Database) HasTables(ctx context.Context, tables []string) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("database is required")
	}
	if len(tables) == 0 {
		return fmt.Errorf("at least one table is required")
	}

	uniqueTables := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		if table == "" {
			return fmt.Errorf("table name is required")
		}
		uniqueTables[table] = struct{}{}
	}

	var count int64
	if err := d.db.WithContext(ctx).
		Table("pg_catalog.pg_tables").
		Where("schemaname = current_schema() AND tablename IN ?", tables).
		Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueTables)) {
		return fmt.Errorf("database schema is incomplete")
	}
	return nil
}

func (d *Database) MigrationReady(ctx context.Context, minimumVersion uint) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("database is required")
	}
	if minimumVersion == 0 {
		return fmt.Errorf("minimum migration version must be greater than zero")
	}

	var version uint
	var dirty bool
	if err := d.db.WithContext(ctx).Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").Row().Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	return validateMigrationState(version, dirty, minimumVersion)
}

func validateMigrationState(version uint, dirty bool, minimumVersion uint) error {
	if dirty {
		return fmt.Errorf("database migration is dirty")
	}
	if version < minimumVersion {
		return fmt.Errorf("database migration version %d is below required minimum %d", version, minimumVersion)
	}
	return nil
}

func (d *Database) WithTransaction(function func(tx IDatabase) error) error {
	return d.WithTransactionContext(context.Background(), function)
}

func (d *Database) WithTransactionContext(ctx context.Context, function func(tx IDatabase) error) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return function(&Database{db: tx})
	})
}

func (d *Database) Preload(query string, args ...interface{}) IDatabase {
	d.db.Preload(query, args...)
	return d
}

func (d *Database) Create(ctx context.Context, doc any) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	return d.db.WithContext(ctx).Create(doc).Error
}

func (d *Database) CreateInBatches(ctx context.Context, docs any, batchSize int) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	return d.db.WithContext(ctx).CreateInBatches(docs, batchSize).Error
}

func (d *Database) Update(ctx context.Context, doc any) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	return d.db.WithContext(ctx).Save(doc).Error
}

func (d *Database) Delete(ctx context.Context, value any, opts ...FindOption) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	query := d.applyOptions(opts...).WithContext(ctx)
	return query.Delete(value).Error
}

func (d *Database) FindById(ctx context.Context, id string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	if err := d.db.WithContext(ctx).Where("id = ? ", id).First(result).Error; err != nil {
		return err
	}

	return nil
}

func (d *Database) FindOne(ctx context.Context, result any, opts ...FindOption) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	query := d.applyOptions(opts...).WithContext(ctx)
	if err := query.First(result).Error; err != nil {
		return err
	}

	return nil
}

func (d *Database) Find(ctx context.Context, result any, opts ...FindOption) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	query := d.applyOptions(opts...).WithContext(ctx)
	if err := query.Find(result).Error; err != nil {
		return err
	}

	return nil
}

func (d *Database) Count(ctx context.Context, model any, total *int64, opts ...FindOption) error {
	ctx, cancel := context.WithTimeout(ctx, DatabaseTimeout)
	defer cancel()

	query := d.applyOptions(opts...).WithContext(ctx)
	if err := query.Model(model).Count(total).Error; err != nil {
		return err
	}

	return nil
}

func (d *Database) GetDB() *gorm.DB {
	return d.db
}

func (d *Database) applyOptions(opts ...FindOption) *gorm.DB {
	query := d.db

	opt := getOption(opts...)

	if len(opt.preloads) != 0 {
		for _, preload := range opt.preloads {
			query = query.Preload(preload)
		}
	}

	if opt.query != nil {
		for _, q := range opt.query {
			query = query.Where(q.Query, q.Args...)
		}
	}

	if opt.order != "" {
		query = query.Order(opt.order)
	}

	if opt.offset != 0 {
		query = query.Offset(opt.offset)
	}

	if opt.limit != 0 {
		query = query.Limit(opt.limit)
	}

	return query
}
