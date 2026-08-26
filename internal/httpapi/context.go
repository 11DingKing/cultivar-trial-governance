package httpapi

import (
	"context"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

type contextKey string

const principalKey contextKey = "principal"

func withPrincipal(ctx context.Context, principal domain.Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

func principalFrom(ctx context.Context) (domain.Principal, bool) {
	value, ok := ctx.Value(principalKey).(domain.Principal)
	return value, ok
}
