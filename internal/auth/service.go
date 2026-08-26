package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type Service struct {
	Store      *store.DB
	Clock      clock.Clock
	IDs        idgen.Generator
	SessionTTL time.Duration
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      domain.User `json:"user"`
}

func (s Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	user, err := s.Store.FindUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return LoginResult{}, apperror.ErrUnauthenticated
		}
		return LoginResult{}, fmt.Errorf("login lookup: %w", err)
	}
	if !user.Active {
		return LoginResult{}, apperror.ErrForbidden
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return LoginResult{}, err
	}
	token, hash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	id, err := s.IDs.New("session")
	if err != nil {
		return LoginResult{}, err
	}
	now := s.Clock.Now()
	session := domain.Session{ID: id, UserID: user.ID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(s.SessionTTL)}
	if err := s.Store.InsertSession(ctx, nil, session); err != nil {
		return LoginResult{}, fmt.Errorf("persist login: %w", err)
	}
	return LoginResult{Token: token, ExpiresAt: session.ExpiresAt, User: user}, nil
}

func (s Service) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	if err := ctx.Err(); err != nil {
		return domain.Principal{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.Principal{}, apperror.ErrUnauthenticated
	}
	session, user, err := s.Store.FindSessionByHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return domain.Principal{}, apperror.ErrUnauthenticated
		}
		return domain.Principal{}, fmt.Errorf("authenticate session: %w", err)
	}
	if err := session.UsableAt(s.Clock.Now()); err != nil {
		return domain.Principal{}, apperror.ErrUnauthenticated
	}
	if !user.Active {
		return domain.Principal{}, apperror.ErrForbidden
	}
	return domain.Principal{UserID: user.ID, InstitutionID: user.InstitutionID, Region: user.Region, Role: user.Role, SessionID: session.ID}, nil
}

func (s Service) Logout(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, _, err := s.Store.FindSessionByHash(ctx, hashToken(strings.TrimSpace(token)))
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("logout lookup: %w", err)
	}
	if session.RevokedAt != nil {
		return nil
	}
	return s.Store.RevokeSession(ctx, session.ID, s.Clock.Now().Format(time.RFC3339Nano))
}

func newToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
