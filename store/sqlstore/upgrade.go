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

	"go.mau.fi/whatsmeow/store/sqlstore/upgrades"
)

// LatestVersion is the latest database schema version.
// This should match the version in 00-latest-schema.sql header (v0 -> vN).
const LatestVersion = 12

// migrationFile represents a SQL migration file with its version number.
type migrationFile struct {
	version  int
	filename string
	content  string
}

// versionRegex matches SQL file names like "03-message-secrets.sql" or "00-latest-schema.sql"
var versionRegex = regexp.MustCompile(`^(\d+)-.*\.sql$`)

// loadMigrations loads all SQL migration files from the embedded filesystem.
func loadMigrations() ([]migrationFile, error) {
	entries, err := fs.ReadDir(upgrades.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []migrationFile
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

	// Sort migrations by version number
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// getLatestSchema returns the content of the 00-latest-schema.sql file.
func getLatestSchema(migrations []migrationFile) (string, bool) {
	for _, m := range migrations {
		if m.version == 0 && strings.Contains(m.filename, "latest-schema") {
			return m.content, true
		}
	}
	return "", false
}

func (clientInstance *ClientInstance) getVersion() (int, error) {
	_, err := clientInstance.DbPool.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS whatsmeow_version (version INTEGER)")
	if err != nil {
		return -1, err
	}

	version := 0
	row := clientInstance.DbPool.QueryRow(context.Background(), "SELECT version FROM whatsmeow_version LIMIT 1")
	if row != nil {
		_ = row.Scan(&version)
	}
	return version, nil
}

func (clientInstance *ClientInstance) setVersion(ctx context.Context, version int) error {
	_, err := clientInstance.DbPool.Exec(ctx, "DELETE FROM whatsmeow_version")
	if err != nil {
		return err
	}
	_, err = clientInstance.DbPool.Exec(ctx, "INSERT INTO whatsmeow_version (version) VALUES ($1)", version)
	return err
}

// Upgrade upgrades the database from the current to the latest version available.
func (clientInstance *ClientInstance) Upgrade() error {
	currentVersion, err := clientInstance.getVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if currentVersion >= LatestVersion {
		clientInstance.Log.Infof("Database already at version %d, no upgrade needed", currentVersion)
		return nil
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	ctx := context.Background()

	// Fresh install: use the latest schema directly
	if currentVersion == 0 {
		latestSchema, found := getLatestSchema(migrations)
		if !found {
			return fmt.Errorf("latest schema file (00-latest-schema.sql) not found")
		}

		clientInstance.Log.Infof("Fresh install: applying latest schema (v%d)", LatestVersion)

		tx, err := clientInstance.DbPool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		_, err = tx.Exec(ctx, latestSchema)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to apply latest schema: %w", err)
		}

		if err = clientInstance.setVersionInTx(ctx, tx, LatestVersion); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to set version: %w", err)
		}

		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		clientInstance.Log.Infof("Database upgraded to v%d", LatestVersion)
		return nil
	}

	// Incremental upgrade: apply migrations one by one
	for _, migration := range migrations {
		// Skip version 0 (latest schema) and already applied versions
		if migration.version == 0 || migration.version <= currentVersion {
			continue
		}

		clientInstance.Log.Infof("Upgrading database to v%d (%s)", migration.version, migration.filename)

		tx, err := clientInstance.DbPool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for v%d: %w", migration.version, err)
		}

		_, err = tx.Exec(ctx, migration.content)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to apply migration v%d (%s): %w", migration.version, migration.filename, err)
		}

		if err = clientInstance.setVersionInTx(ctx, tx, migration.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to set version to %d: %w", migration.version, err)
		}

		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration v%d: %w", migration.version, err)
		}

		currentVersion = migration.version
	}

	clientInstance.Log.Infof("Database upgraded to v%d", currentVersion)
	return nil
}

func (clientInstance *ClientInstance) setVersionInTx(ctx context.Context, tx pgx.Tx, version int) error {
	_, err := tx.Exec(ctx, "DELETE FROM whatsmeow_version")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO whatsmeow_version (version) VALUES ($1)", version)
	return err
}