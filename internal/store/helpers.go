package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

const timestampLayout = time.RFC3339Nano

func formatTime(value time.Time) string { return value.UTC().Format(timestampLayout) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(timestampLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func optionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func translateNotFound(entity, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.WithEntity("store.get", entity, id, apperror.ErrNotFound)
	}
	return err
}

func expectOne(result sql.Result, entity, id string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if count == 0 {
		return apperror.WithEntity("store.update", entity, id, apperror.ErrStaleVersion)
	}
	if count != 1 {
		return fmt.Errorf("update %s %s affected %d rows", entity, id, count)
	}
	return nil
}

type Page struct {
	Limit  int
	Offset int
}

func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 20
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}
