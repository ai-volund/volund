//go:build integration

// Package integration provides end-to-end tests for the Volund gateway.
//
// Tests in this package require Docker to be running. They spin up PostgreSQL
// and NATS containers using testcontainers-go, run migrations, and exercise the
// full HTTP API stack.
//
// Run with: go test -tags integration ./tests/integration/...
package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestEnv holds references to test containers and connection strings.
type TestEnv struct {
	PgURL     string
	NatsURL   string
	PgPool    *pgxpool.Pool
	NatsConn  *nats.Conn
	pgC       testcontainers.Container
	natsC     testcontainers.Container
}

// SetupTestEnv starts PostgreSQL and NATS containers, runs migrations,
// and returns a TestEnv. Call Teardown when done.
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()
	ctx := context.Background()

	// --- PostgreSQL ---
	pgReq := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "volund",
			"POSTGRES_PASSWORD": "volund",
			"POSTGRES_DB":       "volund_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}
	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: pgReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	pgHost, _ := pgC.Host(ctx)
	pgPort, _ := pgC.MappedPort(ctx, "5432")
	pgURL := fmt.Sprintf("postgres://volund:volund@%s:%s/volund_test?sslmode=disable", pgHost, pgPort.Port())

	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		pgC.Terminate(ctx) //nolint:errcheck
		t.Fatalf("connect to postgres: %v", err)
	}

	// Run migrations.
	runMigrations(t, pool)

	// --- NATS ---
	natsReq := testcontainers.ContainerRequest{
		Image:        "nats:2-alpine",
		ExposedPorts: []string{"4222/tcp"},
		WaitingFor:   wait.ForLog("Server is ready").WithStartupTimeout(10 * time.Second),
	}
	natsC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: natsReq,
		Started:          true,
	})
	if err != nil {
		pool.Close()
		pgC.Terminate(ctx) //nolint:errcheck
		t.Fatalf("start nats container: %v", err)
	}

	natsHost, _ := natsC.Host(ctx)
	natsPort, _ := natsC.MappedPort(ctx, "4222")
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	nc, err := nats.Connect(natsURL)
	if err != nil {
		natsC.Terminate(ctx) //nolint:errcheck
		pool.Close()
		pgC.Terminate(ctx) //nolint:errcheck
		t.Fatalf("connect to nats: %v", err)
	}

	return &TestEnv{
		PgURL:    pgURL,
		NatsURL:  natsURL,
		PgPool:   pool,
		NatsConn: nc,
		pgC:      pgC,
		natsC:    natsC,
	}
}

// Teardown cleans up test containers and connections.
func (e *TestEnv) Teardown() {
	ctx := context.Background()
	if e.NatsConn != nil {
		e.NatsConn.Close()
	}
	if e.PgPool != nil {
		e.PgPool.Close()
	}
	if e.natsC != nil {
		e.natsC.Terminate(ctx) //nolint:errcheck
	}
	if e.pgC != nil {
		e.pgC.Terminate(ctx) //nolint:errcheck
	}
}

// runMigrations applies all .up.sql files from the migrations directory.
func runMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	// Find migrations directory (relative to test file or project root).
	migrationsDir := findMigrationsDir(t)

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("reading migrations dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		// Only run .up.sql files.
		if len(entry.Name()) < 7 || entry.Name()[len(entry.Name())-7:] != ".up.sql" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			t.Fatalf("reading migration %s: %v", entry.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("running migration %s: %v", entry.Name(), err)
		}
		t.Logf("applied migration: %s", entry.Name())
	}
}

// findMigrationsDir walks up from the test directory to find internal/db/migrations.
func findMigrationsDir(t *testing.T) string {
	t.Helper()
	// Try relative paths from the test file.
	candidates := []string{
		"../../internal/db/migrations",
		"../internal/db/migrations",
		"internal/db/migrations",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	t.Fatal("could not find migrations directory")
	return ""
}
