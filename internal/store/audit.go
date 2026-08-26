package store

import (
	"context"
	"fmt"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertAudit(ctx context.Context, executor Executor, value domain.AuditEvent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO audit_events(
        id,actor_user_id,institution_id,request_id,action,object_type,object_id,outcome,
        policy_ref,before_json,after_json,created_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ActorUserID, value.InstitutionID,
		value.RequestID, value.Action, value.ObjectType, value.ObjectID, value.Outcome, value.PolicyRef,
		value.BeforeJSON, value.AfterJSON, formatTime(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert audit event %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) ListAuditForObject(ctx context.Context, objectType, objectID string, page Page) ([]domain.AuditEvent, error) {
	page = page.Normalize()
	rows, err := d.sql.QueryContext(ctx, `SELECT id,actor_user_id,institution_id,request_id,action,
        object_type,object_id,outcome,policy_ref,before_json,after_json,created_at
        FROM audit_events WHERE object_type=? AND object_id=? ORDER BY created_at DESC,id LIMIT ? OFFSET ?`,
		objectType, objectID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	values := make([]domain.AuditEvent, 0, page.Limit)
	for rows.Next() {
		var value domain.AuditEvent
		var created string
		if err := rows.Scan(&value.ID, &value.ActorUserID, &value.InstitutionID, &value.RequestID,
			&value.Action, &value.ObjectType, &value.ObjectID, &value.Outcome, &value.PolicyRef,
			&value.BeforeJSON, &value.AfterJSON, &created); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if value.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type IdempotencyRecord struct {
	InstitutionID string
	Method        string
	Path          string
	Key           string
	RequestHash   string
	ResponseCode  int
	ResponseJSON  string
	CreatedAt     string
	ExpiresAt     string
}

func (d *DB) GetIdempotency(ctx context.Context, executor Executor, institutionID, method, path, key string) (IdempotencyRecord, bool, error) {
	if executor == nil {
		executor = d.sql
	}
	var value IdempotencyRecord
	err := executor.QueryRowContext(ctx, `SELECT institution_id,method,path,idem_key,request_hash,
        response_code,response_json,created_at,expires_at FROM idempotency_keys
        WHERE institution_id=? AND method=? AND path=? AND idem_key=?`, institutionID, method, path, key).Scan(
		&value.InstitutionID, &value.Method, &value.Path, &value.Key, &value.RequestHash,
		&value.ResponseCode, &value.ResponseJSON, &value.CreatedAt, &value.ExpiresAt)
	if err != nil {
		if isNoRows(err) {
			return IdempotencyRecord{}, false, nil
		}
		return IdempotencyRecord{}, false, fmt.Errorf("get idempotency key: %w", err)
	}
	return value, true, nil
}

func (d *DB) InsertIdempotency(ctx context.Context, executor Executor, value IdempotencyRecord) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO idempotency_keys(
        institution_id,method,path,idem_key,request_hash,response_code,response_json,created_at,expires_at
    ) VALUES(?,?,?,?,?,?,?,?,?)`, value.InstitutionID, value.Method, value.Path, value.Key,
		value.RequestHash, value.ResponseCode, value.ResponseJSON, value.CreatedAt, value.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert idempotency key: %w", err)
	}
	return nil
}

func isNoRows(err error) bool {
	return err != nil && err.Error() == "sql: no rows in result set"
}

func (d *DB) PurgeIdempotency(ctx context.Context, before string) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at<=?`, before)
	if err != nil {
		return 0, fmt.Errorf("purge idempotency keys: %w", err)
	}
	return result.RowsAffected()
}
