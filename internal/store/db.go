package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/migrate"
	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("database DSN is empty")
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(8)
	database.SetConnMaxLifetime(30 * time.Minute)
	cleanup := func(cause error) (*DB, error) {
		_ = database.Close()
		return nil, cause
	}
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, pragma := range pragmas {
		if _, err := database.ExecContext(ctx, pragma); err != nil {
			return cleanup(fmt.Errorf("configure sqlite with %q: %w", pragma, err))
		}
	}
	if err := migrate.Apply(ctx, database); err != nil {
		return cleanup(fmt.Errorf("apply migrations: %w", err))
	}
	if err := database.PingContext(ctx); err != nil {
		return cleanup(fmt.Errorf("ping sqlite: %w", err))
	}
	return &DB{sql: database}, nil
}

func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

func (d *DB) Ping(ctx context.Context) error {
	if d == nil || d.sql == nil {
		return errors.New("database is not initialized")
	}
	return d.sql.PingContext(ctx)
}

func (d *DB) SQL() *sql.DB { return d.sql }

// WithTx retries only a complete callback when SQLite reports transient busy.
// Callers must allocate stable IDs before entering the callback so replay is safe.
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			last = err
			if !isBusy(err) {
				return fmt.Errorf("begin transaction: %w", err)
			}
		} else {
			err = fn(tx)
			if err == nil {
				if commitErr := tx.Commit(); commitErr == nil {
					return nil
				} else {
					err = commitErr
				}
			} else {
				_ = tx.Rollback()
			}
			last = err
			if !isBusy(err) {
				return err
			}
		}
		delay := time.Duration(attempt+1) * 15 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("transaction remained busy: %w", last)
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "database is busy") || strings.Contains(text, "sqlite_busy")
}
