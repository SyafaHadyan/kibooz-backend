// Package db connects to PostgreSQL and applies the embedded SQL migrations
package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// dsnValue quotes a value of a keyword/value connection string, so spaces, quotes and backslashes survive parsing
func dsnValue(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}

// idleInTransactionTimeout is how long the server lets a transaction sit idle before it ends the session and frees its
// locks. Statements and lock waits are bounded by the deadline of the request instead of a session setting, because the
// migrations run on the same connections and an index build may take longer than any request.
const idleInTransactionTimeout = 30 * time.Second

func buildDSN(cfg *env.Env) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC idle_in_transaction_session_timeout=%d",
		dsnValue(cfg.DBHost), cfg.DBPort, dsnValue(cfg.DBUsername), dsnValue(cfg.DBPassword),
		dsnValue(cfg.DBName), dsnValue(cfg.DBSSLMode), idleInTransactionTimeout.Milliseconds(),
	)
}

func New(cfg *env.Env) (*gorm.DB, error) {
	dsn := buildDSN(cfg)

	gormLogger := logger.New(
		log.New(os.Stdout, "", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		},
	)

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:      gormLogger,
		NowFunc:     func() time.Time { return time.Now().UTC() },
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql handle: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	err = sqlDB.Ping()
	if err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return database, nil
}

// Migrate applies every pending migration and is safe to call on each startup
func Migrate(sqlDB *sql.DB) error {
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	err = migrator.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
