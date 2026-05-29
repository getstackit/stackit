// Package testpostgres provisions a Postgres backend for store tests.
//
// It is test-only support code and intentionally the ONLY place in the store
// tree that imports testcontainers, keeping that heavy, Docker-bound
// dependency out of the shipped server binary's dependency graph.
package testpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const image = "postgres:18"

// ErrUnavailable means no Postgres backend could be provisioned: neither
// STACKIT_TEST_DATABASE_URL is set nor could a throwaway container start.
// Callers should t.Skip on this so the suite stays green without Docker.
var ErrUnavailable = errors.New("no postgres test backend available")

// Harness owns the connection string for store tests and serializes access so
// parallel tests don't observe each other's rows.
type Harness struct {
	URL string

	container *postgres.PostgresContainer // nil when using an external DB
	mu        sync.Mutex
}

// Start provisions a backend. Precedence:
//  1. STACKIT_TEST_DATABASE_URL (e.g. a CI service container) — used directly.
//  2. A throwaway testcontainer (local dev with a running Docker daemon).
//
// It returns ErrUnavailable when neither is possible.
func Start(ctx context.Context) (*Harness, error) {
	if url := strings.TrimSpace(os.Getenv("STACKIT_TEST_DATABASE_URL")); url != "" {
		return &Harness{URL: url}, nil
	}

	// Ryuk (the testcontainers reaper) is itself a container; disabling it
	// avoids a second image pull. TestMain terminates our container directly.
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		if err := os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true"); err != nil {
			return nil, fmt.Errorf("disable ryuk: %w", err)
		}
	}

	container, err := postgres.Run(
		ctx,
		image,
		postgres.BasicWaitStrategies(),
		postgres.WithDatabase("stackit_test"),
		postgres.WithUsername("stackit"),
		postgres.WithPassword("stackit"),
		postgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		// A failure here almost always means Docker isn't running. Treat it
		// as "no backend" rather than a hard error so the suite can skip.
		return nil, fmt.Errorf("%w: start postgres container: %w", ErrUnavailable, err)
	}

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("postgres test connection string: %w", err)
	}

	return &Harness{URL: url, container: container}, nil
}

// Terminate releases a container-backed harness. External-DB harnesses are a
// no-op.
func (h *Harness) Terminate(ctx context.Context) error {
	if h == nil || h.container == nil {
		return nil
	}
	return h.container.Terminate(ctx)
}

// Reset drops the store's tables so the next store.Open re-runs migrations
// against an empty schema. It holds an exclusive lock for the duration of the
// calling test, serializing DB access across parallel tests.
func (h *Harness) Reset(t testing.TB) {
	t.Helper()

	h.mu.Lock()
	t.Cleanup(h.mu.Unlock)

	db, err := sql.Open("pgx", h.URL)
	if err != nil {
		t.Fatalf("open postgres for reset: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if _, err := db.ExecContext(
		context.Background(),
		"DROP TABLE IF EXISTS repos, schema_migrations CASCADE",
	); err != nil {
		t.Fatalf("reset postgres schema: %v", err)
	}
}
