package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertJob(ctx context.Context, executor Executor, value domain.WorkerJob) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO worker_jobs(
        id,job_type,object_type,object_id,payload_json,status,attempts,max_attempts,available_at,
        lease_owner,lease_expires_at,last_error,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.Type, value.ObjectType, value.ObjectID,
		value.PayloadJSON, value.Status, value.Attempts, value.MaxAttempts, formatTime(value.AvailableAt),
		value.LeaseOwner, nil, value.LastError, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert worker job %s: %w", value.ID, err)
	}
	return nil
}

func scanJob(scanner interface{ Scan(...any) error }) (domain.WorkerJob, error) {
	var value domain.WorkerJob
	var jobType, status, available, created, updated string
	var leaseExpires sql.NullString
	err := scanner.Scan(&value.ID, &jobType, &value.ObjectType, &value.ObjectID, &value.PayloadJSON,
		&status, &value.Attempts, &value.MaxAttempts, &available, &value.LeaseOwner,
		&leaseExpires, &value.LastError, &created, &updated)
	if err != nil {
		return domain.WorkerJob{}, err
	}
	value.Type = domain.JobType(jobType)
	value.Status = domain.JobStatus(status)
	if value.AvailableAt, err = parseTime(available); err != nil {
		return domain.WorkerJob{}, err
	}
	if value.LeaseExpiresAt, err = optionalTime(leaseExpires); err != nil {
		return domain.WorkerJob{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.WorkerJob{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.WorkerJob{}, err
	}
	return value, nil
}

const jobColumns = `id,job_type,object_type,object_id,payload_json,status,attempts,max_attempts,
    available_at,lease_owner,lease_expires_at,last_error,created_at,updated_at`

func (d *DB) GetJob(ctx context.Context, id string) (domain.WorkerJob, error) {
	value, err := scanJob(d.sql.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM worker_jobs WHERE id=?`, id))
	if err != nil {
		return domain.WorkerJob{}, translateNotFound("worker_job", id, err)
	}
	return value, nil
}

func (d *DB) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `UPDATE worker_jobs SET
        status='pending',lease_owner='',lease_expires_at=NULL,available_at=?,updated_at=?,
        last_error=CASE WHEN last_error='' THEN 'lease expired before completion' ELSE last_error END
        WHERE status='running' AND lease_expires_at IS NOT NULL AND lease_expires_at<=?`,
		formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("recover expired worker leases: %w", err)
	}
	return result.RowsAffected()
}

func (d *DB) ClaimJob(ctx context.Context, owner string, now time.Time, lease time.Duration) (domain.WorkerJob, error) {
	var claimed domain.WorkerJob
	err := d.WithTx(ctx, func(tx *sql.Tx) error {
		candidate, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM worker_jobs
            WHERE status IN ('pending','failed') AND available_at<=? AND attempts<max_attempts
            ORDER BY available_at,id LIMIT 1`, formatTime(now)))
		if err != nil {
			if err == sql.ErrNoRows {
				return apperror.ErrNotFound
			}
			return fmt.Errorf("select claimable worker job: %w", err)
		}
		expires := now.Add(lease)
		result, err := tx.ExecContext(ctx, `UPDATE worker_jobs SET status='running',attempts=attempts+1,
            lease_owner=?,lease_expires_at=?,updated_at=? WHERE id=? AND status IN ('pending','failed')`,
			owner, formatTime(expires), formatTime(now), candidate.ID)
		if err != nil {
			return fmt.Errorf("claim worker job %s: %w", candidate.ID, err)
		}
		if err := expectOne(result, "worker_job", candidate.ID); err != nil {
			return err
		}
		candidate.Status = domain.JobRunning
		candidate.Attempts++
		candidate.LeaseOwner = owner
		candidate.LeaseExpiresAt = &expires
		candidate.UpdatedAt = now
		claimed = candidate
		return nil
	})
	return claimed, err
}

func (d *DB) CompleteJob(ctx context.Context, id, owner string, now time.Time) error {
	result, err := d.sql.ExecContext(ctx, `UPDATE worker_jobs SET status='completed',lease_owner='',
        lease_expires_at=NULL,last_error='',updated_at=? WHERE id=? AND status='running' AND lease_owner=?`,
		formatTime(now), id, owner)
	if err != nil {
		return fmt.Errorf("complete worker job %s: %w", id, err)
	}
	return expectOne(result, "worker_job", id)
}

func (d *DB) FailJob(ctx context.Context, value domain.WorkerJob, owner string, cause error, permanent bool, now time.Time) error {
	status := domain.JobFailed
	available := value.RetryAt(now)
	if permanent || value.Attempts >= value.MaxAttempts {
		status = domain.JobDead
		available = now
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE worker_jobs SET status=?,lease_owner='',lease_expires_at=NULL,
        available_at=?,last_error=?,updated_at=? WHERE id=? AND status='running' AND lease_owner=?`,
		status, formatTime(available), truncateError(cause), formatTime(now), value.ID, owner)
	if err != nil {
		return fmt.Errorf("fail worker job %s: %w", value.ID, err)
	}
	return expectOne(result, "worker_job", value.ID)
}

func truncateError(err error) string {
	if err == nil {
		return "worker failed without an error"
	}
	text := err.Error()
	if len(text) > 1000 {
		text = text[:1000]
	}
	return text
}

func (d *DB) ListDueObservationBatches(ctx context.Context, before time.Time, limit int) ([]domain.ObservationBatch, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id,application_id,window_name,opens_at,closes_at,
        status,version,locked_at,created_at,updated_at FROM observation_batches
        WHERE status='open' AND closes_at<=? ORDER BY closes_at LIMIT ?`, formatTime(before), limit)
	if err != nil {
		return nil, fmt.Errorf("list due observation batches: %w", err)
	}
	defer rows.Close()
	values := make([]domain.ObservationBatch, 0, limit)
	for rows.Next() {
		value, err := scanObservationBatch(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
