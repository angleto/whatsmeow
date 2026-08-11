// Copyright (c) 2021 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sqlstore

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.mau.fi/whatsmeow/store/sqlstore/upgrades"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// LatestVersion is the schema version recorded in whatsmeow_version. It must equal the
// highest numbered migration file and the version declared in the 00-latest-schema.sql
// header; loadMigrations refuses to start otherwise.
//
// It is bookkeeping, not the source of truth: which migrations have run is recorded
// per file in whatsmeow_migrations. Version numbers have been reused with different
// meanings across released branches of this fork (v2.4 stamps 15 for
// privacy-token-sender-timestamp, this branch stamps 15 for companion-meta-nonce, and
// two different files were once both numbered 12), so a single high-water mark cannot
// answer which files a given database has actually applied.
const LatestVersion = 16

// migrationAdvisoryLockID serializes concurrent Upgrade calls against one database.
// Without it, several pods starting at once run the same CREATE INDEX IF NOT EXISTS
// concurrently, and the losers abort with a duplicate key error on
// pg_class_relname_nsp_index. The value is arbitrary but must stay stable.
const migrationAdvisoryLockID int64 = 0x77686d77

// createMigrationsTableQuery creates the per-migration ledger. Like whatsmeow_version
// it is global bookkeeping rather than tenant data, so it has no business_id column
// and no row level security policy.
const createMigrationsTableQuery = `
	CREATE TABLE IF NOT EXISTS whatsmeow_migrations (
		filename   TEXT        NOT NULL PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)
`

const (
	selectAppliedMigrationsQuery = `SELECT filename FROM whatsmeow_migrations`
	insertMigrationQuery         = `INSERT INTO whatsmeow_migrations (filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING`
)

// migrationFile represents a SQL migration file with its version number.
type migrationFile struct {
	version  int
	filename string
	content  string
}

// versionRegex matches SQL file names like "03-message-secrets.sql" or "00-latest-schema.sql"
var versionRegex = regexp.MustCompile(`^(\d+)-.*\.sql$`)

// latestSchemaHeaderRegex matches the "-- v0 -> v16" header of 00-latest-schema.sql.
var latestSchemaHeaderRegex = regexp.MustCompile(`(?m)^--\s*v0\s*->\s*v(\d+)`)

// loadMigrations loads and validates all SQL migration files from the embedded
// filesystem. It runs on every Upgrade call, including when the database turns out to
// need no work, so a packaging mistake is reported everywhere rather than only on the
// deployments that happen to be behind.
func loadMigrations() ([]migrationFile, error) {
	entries, err := fs.ReadDir(upgrades.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []migrationFile
	// Migrations are applied in version order, so two files sharing a version number
	// have no defined order between them. Refuse rather than pick one arbitrarily.
	seen := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		matches := versionRegex.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("duplicate migration version %d: %s and %s", version, other, entry.Name())
		}
		seen[version] = entry.Name()

		content, err := fs.ReadFile(upgrades.FS, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, migrationFile{
			version:  version,
			filename: entry.Name(),
			content:  string(content),
		})
	}

	if len(migrations) == 0 {
		return nil, fmt.Errorf("no migration files found in embedded filesystem")
	}

	// Versions are unique by the check above, so the order is total; SliceStable keeps
	// it deterministic regardless.
	sort.SliceStable(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Both directions matter. A file above LatestVersion would never be reached by the
	// fresh-install stamp; a LatestVersion above every file would stamp fresh installs
	// with a version whose migration does not exist yet.
	if highest := migrations[len(migrations)-1]; highest.version != LatestVersion {
		return nil, fmt.Errorf("highest migration %s is v%d but LatestVersion is %d; they must be equal",
			highest.filename, highest.version, LatestVersion)
	}

	return migrations, nil
}

// getLatestSchema returns the content of the 00-latest-schema.sql file, checking that
// the version it declares in its header matches LatestVersion.
func getLatestSchema(migrations []migrationFile) (string, error) {
	for _, m := range migrations {
		if m.version != 0 || !strings.Contains(m.filename, "latest-schema") {
			continue
		}
		matches := latestSchemaHeaderRegex.FindStringSubmatch(m.content)
		if matches == nil {
			return "", fmt.Errorf("%s has no '-- v0 -> vN' version header", m.filename)
		}
		declared, err := strconv.Atoi(matches[1])
		if err != nil {
			return "", fmt.Errorf("%s has an unparseable version header: %w", m.filename, err)
		}
		if declared != LatestVersion {
			return "", fmt.Errorf("%s declares v%d but LatestVersion is %d", m.filename, declared, LatestVersion)
		}
		return m.content, nil
	}
	return "", fmt.Errorf("latest schema file (00-latest-schema.sql) not found")
}

func (clientInstance *ClientInstance) getVersion(ctx context.Context, conn *pgxpool.Conn) (int, error) {
	_, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS whatsmeow_version (version INTEGER)")
	if err != nil {
		return -1, err
	}

	version := 0
	row := conn.QueryRow(ctx, "SELECT version FROM whatsmeow_version LIMIT 1")
	if row != nil {
		_ = row.Scan(&version)
	}
	return version, nil
}

func (clientInstance *ClientInstance) setVersionInTx(ctx context.Context, tx pgx.Tx, version int) error {
	_, err := tx.Exec(ctx, "DELETE FROM whatsmeow_version")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO whatsmeow_version (version) VALUES ($1)", version)
	return err
}

// Upgrade brings the database up to the latest schema.
//
// Which migrations have already run is tracked per file in whatsmeow_migrations. A
// database that predates that table is reconciled by replaying every migration once:
// they are all idempotent, so this is a no-op for the objects that already exist and it
// converges databases created by any released branch of this fork onto the current
// schema, regardless of what their whatsmeow_version happens to say.
//
// Concurrent calls against the same database are serialized with a Postgres advisory
// lock, so a multi-pod rollout cannot race on the DDL.
func (clientInstance *ClientInstance) Upgrade() error {
	if clientInstance.Log == nil {
		// Upgrade logs from a deferred function, where a nil logger would panic and
		// hide whatever actually went wrong.
		clientInstance.Log = waLog.Noop
	}

	// Validate the migration set before touching the database.
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	ctx := context.Background()

	conn, err := clientInstance.DbPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire migration connection: %w", err)
	}
	// Registered first so it runs last: the advisory lock must be released before the
	// connection goes back to the pool, since pgx does not reset session-level locks.
	defer conn.Release()

	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("failed to take the migration lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID); unlockErr != nil {
			clientInstance.Log.Warnf("Failed to release the migration lock: %v", unlockErr)
		}
	}()

	if _, err = conn.Exec(ctx, createMigrationsTableQuery); err != nil {
		return fmt.Errorf("failed to create the migrations table: %w", err)
	}

	applied, err := clientInstance.getAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		currentVersion, versionErr := clientInstance.getVersion(ctx, conn)
		if versionErr != nil {
			return fmt.Errorf("failed to get current version: %w", versionErr)
		}
		if currentVersion == 0 {
			return clientInstance.freshInstall(ctx, conn, migrations)
		}
		// A database from before the ledger existed. Its recorded version cannot be
		// trusted to say which files ran, so replay them all; every migration is
		// idempotent.
		clientInstance.Log.Infof("Reconciling a pre-ledger database (recorded version %d) by replaying all migrations", currentVersion)
	}

	return clientInstance.applyPending(ctx, conn, migrations, applied)
}

func (clientInstance *ClientInstance) getAppliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, selectAppliedMigrationsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to read the migrations table: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var filename string
		if err = rows.Scan(&filename); err != nil {
			return nil, fmt.Errorf("failed to scan the migrations table: %w", err)
		}
		applied[filename] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate the migrations table: %w", err)
	}
	return applied, nil
}

// freshInstall applies 00-latest-schema.sql, which already contains everything every
// migration would build, and records all of them as applied.
func (clientInstance *ClientInstance) freshInstall(ctx context.Context, conn *pgxpool.Conn, migrations []migrationFile) error {
	latestSchema, err := getLatestSchema(migrations)
	if err != nil {
		return err
	}

	clientInstance.Log.Infof("Fresh install: applying latest schema (v%d)", LatestVersion)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, latestSchema); err != nil {
		return fmt.Errorf("failed to apply latest schema: %w", err)
	}
	for _, migration := range migrations {
		if migration.version == 0 {
			continue
		}
		if _, err = tx.Exec(ctx, insertMigrationQuery, migration.filename); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", migration.filename, err)
		}
	}
	if err = clientInstance.setVersionInTx(ctx, tx, LatestVersion); err != nil {
		return fmt.Errorf("failed to set version: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	clientInstance.Log.Infof("Database installed at v%d", LatestVersion)
	return nil
}

// applyPending runs every migration that the ledger does not already list, in version
// order, recording each one in the same transaction that applies it.
func (clientInstance *ClientInstance) applyPending(ctx context.Context, conn *pgxpool.Conn, migrations []migrationFile, applied map[string]struct{}) error {
	count := 0
	for _, migration := range migrations {
		if migration.version == 0 {
			continue
		}
		if _, done := applied[migration.filename]; done {
			continue
		}

		clientInstance.Log.Infof("Applying migration %s", migration.filename)

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", migration.filename, err)
		}

		if _, err = tx.Exec(ctx, migration.content); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to apply migration %s: %w", migration.filename, err)
		}
		if _, err = tx.Exec(ctx, insertMigrationQuery, migration.filename); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s: %w", migration.filename, err)
		}
		if err = clientInstance.setVersionInTx(ctx, tx, LatestVersion); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to set version while applying %s: %w", migration.filename, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", migration.filename, err)
		}
		count++
	}

	if count == 0 {
		clientInstance.Log.Infof("Database already at v%d, no migrations to apply", LatestVersion)
	} else {
		clientInstance.Log.Infof("Applied %d migration(s), database at v%d", count, LatestVersion)
	}
	return nil
}
