// Package db manages the PostgreSQL connection pool and schema migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Pool wraps a pgxpool.Pool.
type Pool struct {
	*pgxpool.Pool
}

// WrapPool wraps an existing pgxpool.Pool for use with Volund repos.
// Useful for tests that manage their own pool lifecycle.
func WrapPool(p *pgxpool.Pool) *Pool { return &Pool{p} }

// Connect creates a connection pool and verifies connectivity.
func Connect(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return &Pool{pool}, nil
}

// Migrate runs all pending up-migrations embedded in the binary.
func Migrate(dsn string) error {
	src, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("db migrate: source: %w", err)
	}

	// golang-migrate pgx5 driver expects "pgx5://user:pass@host/db" — replace
	// "postgres://" or "postgresql://" prefix with "pgx5://".
	migrateURL := dsn
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(dsn, prefix) {
			migrateURL = "pgx5://" + strings.TrimPrefix(dsn, prefix)
			break
		}
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		return fmt.Errorf("db migrate: init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("db migrate: up: %w", err)
	}
	return nil
}
