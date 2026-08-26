package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

func List() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	result := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q has no numeric prefix", entry.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q has invalid version", entry.Name())
		}
		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(content)
		result = append(result, Migration{Version: version, Name: entry.Name(), SQL: string(content), Checksum: hex.EncodeToString(digest[:])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for index := range result {
		if index > 0 && result[index-1].Version == result[index].Version {
			return nil, fmt.Errorf("duplicate migration version %d", result[index].Version)
		}
	}
	return result, nil
}

func Apply(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migration database is nil")
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        checksum TEXT NOT NULL,
        applied_at TEXT NOT NULL
    )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	migrations, err := List()
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migration context: %w", err)
		}
		var existing string
		err := db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = ?", migration.Version).Scan(&existing)
		switch {
		case err == nil:
			if existing != migration.Checksum {
				return fmt.Errorf("migration %d checksum mismatch: stored %s current %s", migration.Version, existing, migration.Checksum)
			}
			continue
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read migration %d: %w", migration.Version, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute migration %d: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES(?,?,?,?)", migration.Version, migration.Name, migration.Checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func CurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}
