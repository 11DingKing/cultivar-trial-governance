package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
	"github.com/11DingKing/cultivar-trial-governance/internal/auth"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
)

type middleware struct {
	Auth   auth.Service
	IDs    idgen.Generator
	Logger *slog.Logger
}

func (m middleware) request(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			generated, err := m.IDs.New("request")
			if err != nil {
				writeError(r.Context(), w, fmt.Errorf("generate request id: %w", err))
				return
			}
			requestID = generated
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := audit.WithRequestID(r.Context(), requestID)
		defer func() {
			if recovered := recover(); recovered != nil {
				m.log().ErrorContext(ctx, "panic recovered", "panic", recovered, "stack", string(debug.Stack()), "request_id", requestID)
				writeError(ctx, w, fmt.Errorf("panic: %v", recovered))
			}
			m.log().InfoContext(ctx, "http request", "method", r.Method, "path", r.URL.Path,
				"request_id", requestID, "duration_ms", time.Since(started).Milliseconds())
		}()
		requestContext := r.WithContext(ctx)
		next.ServeHTTP(w, requestContext)
	})
}

func (m middleware) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		principal, err := m.Auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(r.Context(), w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
	})
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func (m middleware) log() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.Default()
}
