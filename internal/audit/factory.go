package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
)

type contextKey string

const requestIDKey contextKey = "audit-request-id"

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	if value == "" {
		return "internal"
	}
	return value
}

type Factory struct {
	IDs idgen.Generator
}

type Change struct {
	Actor      domain.Principal
	Action     string
	ObjectType string
	ObjectID   string
	Outcome    string
	PolicyRef  string
	Before     any
	After      any
	OccurredAt time.Time
}

func (f Factory) Event(ctx context.Context, change Change) (domain.AuditEvent, error) {
	id, err := f.IDs.New("audit")
	if err != nil {
		return domain.AuditEvent{}, err
	}
	before, err := encode(change.Before)
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("encode audit before state: %w", err)
	}
	after, err := encode(change.After)
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("encode audit after state: %w", err)
	}
	event := domain.AuditEvent{
		ID: id, ActorUserID: change.Actor.UserID, InstitutionID: change.Actor.InstitutionID,
		RequestID: RequestID(ctx), Action: change.Action, ObjectType: change.ObjectType,
		ObjectID: change.ObjectID, Outcome: change.Outcome, PolicyRef: change.PolicyRef,
		BeforeJSON: before, AfterJSON: after, CreatedAt: change.OccurredAt.UTC(),
	}
	if err := event.Validate(); err != nil {
		return domain.AuditEvent{}, err
	}
	return event, nil
}

func encode(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
