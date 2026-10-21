// Package db connects to PostgreSQL and applies the embedded SQL migrations
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver that NewHealth opens
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
		// the default logger, so a slow query is a line of the same JSON log as everything else
		log.Default(),
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

// NewHealth opens a pool of one connection that only the health check uses. The check answers whether the database can be
// reached, and on the shared pool it would instead wait behind the requests whenever all 25 connections are busy, so a busy
// API would look like a dead database. It opens the connection once, so a database that refuses it, for example because it
// has no connection left, stops the start-up and does not show up first as a failed health check.
func NewHealth(cfg *env.Env) (*sql.DB, error) {
	pool, err := sql.Open("pgx", buildDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("open health connection: %w", err)
	}

	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), healthPingTimeout)
	defer cancel()

	err = pool.PingContext(ctx)
	if err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("ping database on the health connection: %w", err)
	}

	return pool, nil
}

// healthPingTimeout bounds the first use of the health connection at start-up
const healthPingTimeout = 5 * time.Second

// newMigrator opens the embedded migrations on a connection
func newMigrator(sqlDB *sql.DB) (*migrate.Migrate, error) {
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		return nil, fmt.Errorf("create migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}

	return migrator, nil
}

// Migrate applies every pending migration and is safe to call on each startup
func Migrate(sqlDB *sql.DB) error {
	migrator, err := newMigrator(sqlDB)
	if err != nil {
		return err
	}

	err = migrator.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
