package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type Service struct {
	Store             *store.DB
	Clock             clock.Clock
	IDs               idgen.Generator
	Audit             audit.Factory
	WorkerMaxAttempts int
}

func (s Service) validate() error {
	if s.Store == nil || s.Clock == nil || s.IDs == nil || s.Audit.IDs == nil {
		return errors.New("service dependencies are incomplete")
	}
	if s.WorkerMaxAttempts < 1 {
		return errors.New("worker max attempts must be positive")
	}
	return nil
}

func (s Service) auditEvent(ctx context.Context, actor domain.Principal, action, objectType, objectID, policy string, before, after any) (domain.AuditEvent, error) {
	return s.Audit.Event(ctx, audit.Change{
		Actor: actor, Action: action, ObjectType: objectType, ObjectID: objectID,
		Outcome: "success", PolicyRef: policy, Before: before, After: after, OccurredAt: s.Clock.Now(),
	})
}

func requestDigest(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode idempotent request: %w", err)
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func decodeStored[T any](record store.IdempotencyRecord) (T, error) {
	var value T
	if err := json.Unmarshal([]byte(record.ResponseJSON), &value); err != nil {
		return value, fmt.Errorf("decode idempotent response: %w", err)
	}
	return value, nil
}

func encodeStored(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode idempotent response: %w", err)
	}
	return string(content), nil
}

func ensureIdempotencyKey(key string) error {
	key = strings.TrimSpace(key)
	if len(key) < 8 || len(key) > 128 {
		return fmt.Errorf("idempotency key length: %w", apperror.ErrValidation)
	}
	return nil
}

func checkContext(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s cancelled: %w", op, err)
	}
	return nil
}

func sameHash(record store.IdempotencyRecord, digest string) error {
	if record.RequestHash != digest {
		return fmt.Errorf("idempotency key reused with different request: %w", apperror.ErrConflict)
	}
	return nil
}

func expiry(now time.Time) string { return now.Add(24 * time.Hour).Format(time.RFC3339Nano) }
