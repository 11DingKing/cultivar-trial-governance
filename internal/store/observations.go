package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertObservationBatch(ctx context.Context, executor Executor, value domain.ObservationBatch) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO observation_batches(
        id,application_id,window_name,opens_at,closes_at,status,version,locked_at,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ApplicationID, value.WindowName, formatTime(value.OpensAt),
		formatTime(value.ClosesAt), value.Status, value.Version, nil, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert observation batch %s: %w", value.ID, err)
	}
	return nil
}

func scanObservationBatch(scanner interface{ Scan(...any) error }) (domain.ObservationBatch, error) {
	var value domain.ObservationBatch
	var opens, closes, status, created, updated string
	var locked sql.NullString
	err := scanner.Scan(&value.ID, &value.ApplicationID, &value.WindowName, &opens, &closes,
		&status, &value.Version, &locked, &created, &updated)
	if err != nil {
		return domain.ObservationBatch{}, err
	}
	value.Status = domain.ObservationBatchStatus(status)
	if value.OpensAt, err = parseTime(opens); err != nil {
		return domain.ObservationBatch{}, err
	}
	if value.ClosesAt, err = parseTime(closes); err != nil {
		return domain.ObservationBatch{}, err
	}
	if value.LockedAt, err = optionalTime(locked); err != nil {
		return domain.ObservationBatch{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.ObservationBatch{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.ObservationBatch{}, err
	}
	return value, nil
}

func (d *DB) GetObservationBatch(ctx context.Context, executor Executor, id string) (domain.ObservationBatch, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanObservationBatch(executor.QueryRowContext(ctx, `SELECT id,application_id,window_name,
        opens_at,closes_at,status,version,locked_at,created_at,updated_at FROM observation_batches WHERE id=?`, id))
	if err != nil {
		return domain.ObservationBatch{}, translateNotFound("observation_batch", id, err)
	}
	return value, nil
}

func (d *DB) UpdateObservationBatch(ctx context.Context, executor Executor, value domain.ObservationBatch, expectedVersion int64) error {
	if executor == nil {
		executor = d.sql
	}
	var locked any
	if value.LockedAt != nil {
		locked = formatTime(*value.LockedAt)
	}
	result, err := executor.ExecContext(ctx, `UPDATE observation_batches SET status=?,version=?,locked_at=?,updated_at=?
        WHERE id=? AND version=?`, value.Status, value.Version, locked, formatTime(value.UpdatedAt), value.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update observation batch %s: %w", value.ID, err)
	}
	return expectOne(result, "observation_batch", value.ID)
}

func (d *DB) InsertObservation(ctx context.Context, executor Executor, value domain.Observation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO observations(
        id,batch_id,metric,value,unit,observed_at,reported_by,anomalous,invalidated,created_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.BatchID, value.Metric, value.Value, value.Unit,
		formatTime(value.ObservedAt), value.ReportedBy, boolInt(value.Anomalous), boolInt(value.Invalidated), formatTime(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert observation %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) ListObservations(ctx context.Context, batchID string, includeInvalid bool) ([]domain.Observation, error) {
	query := `SELECT id,batch_id,metric,value,unit,observed_at,reported_by,anomalous,invalidated,created_at
        FROM observations WHERE batch_id=?`
	if !includeInvalid {
		query += ` AND invalidated=0`
	}
	query += ` ORDER BY observed_at,metric,id`
	rows, err := d.sql.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()
	values := make([]domain.Observation, 0)
	for rows.Next() {
		var value domain.Observation
		var observed, created string
		var anomalous, invalidated int
		if err := rows.Scan(&value.ID, &value.BatchID, &value.Metric, &value.Value, &value.Unit,
			&observed, &value.ReportedBy, &anomalous, &invalidated, &created); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		value.Anomalous = anomalous == 1
		value.Invalidated = invalidated == 1
		if value.ObservedAt, err = parseTime(observed); err != nil {
			return nil, err
		}
		if value.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (d *DB) CountAnomalies(ctx context.Context, executor Executor, batchID string) (int, error) {
	if executor == nil {
		executor = d.sql
	}
	var count int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations WHERE batch_id=? AND anomalous=1 AND invalidated=0`, batchID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count anomalies for %s: %w", batchID, err)
	}
	return count, nil
}
