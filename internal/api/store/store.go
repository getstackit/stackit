// Package store is the API server's PostgreSQL-backed repo configuration
// store. It is a concrete integration adapter (analogous to internal/github):
// it owns the SQL connection, embedded migrations, and the sqlc-generated
// query layer. Only the server/api layer consumes it; nothing in core
// (engine/git) or actions may import it.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/getstackit/stackit/internal/api/store/reposdb"
)

const (
	pgDriverName          = "pgx"
	schemaMigrationsTable = "schema_migrations"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store holds the Postgres connection pool and the generated query layer.
type Store struct {
	db      *sql.DB
	queries *reposdb.Queries
}

// Open connects to Postgres at connString, applies pending migrations, and
// returns a ready-to-use Store. The connection is closed if migrations fail.
func Open(ctx context.Context, connString string) (*Store, error) {
	connString = strings.TrimSpace(connString)
	if connString == "" {
		return nil, fmt.Errorf("postgres connection string is required")
	}

	db, err := sql.Open(pgDriverName, connString)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	store := &Store{
		db:      db,
		queries: reposdb.New(db),
	}

	if err := store.runMigrations(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB exposes the underlying pool so other server-scoped stores (e.g. a future
// session store) can share the same connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// runMigrations applies the embedded migrations using the existing pool, so no
// second connection is opened. migrate.ErrNoChange is expected and ignored.
func (s *Store) runMigrations() error {
	dbDriver, err := migratepostgres.WithInstance(s.db, &migratepostgres.Config{
		MigrationsTable: schemaMigrationsTable,
	})
	if err != nil {
		return fmt.Errorf("create postgres migrate driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open store migrations: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return fmt.Errorf("construct postgres migrator: %w", err)
	}

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run store migrations: %w", err)
	}
	return nil
}
