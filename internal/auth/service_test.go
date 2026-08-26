package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

func authFixture(t *testing.T) (*store.DB, Service, *clock.Manual, domain.User) {
	t.Helper()
	db, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	institution := domain.Institution{ID: "institution", Name: "育种中心", Region: "north", Active: true, CreatedAt: now}
	hash, err := HashPassword("strong-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	user := domain.User{
		ID: "user", InstitutionID: institution.ID, Email: "breeder@example.test", DisplayName: "育种员",
		Role: domain.RoleBreeder, Region: "north", Active: true, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertInstitution(context.Background(), nil, institution); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertUser(context.Background(), nil, user); err != nil {
		t.Fatal(err)
	}
	service := Service{Store: db, Clock: manual, IDs: idgen.NewSequence(1), SessionTTL: time.Hour}
	return db, service, manual, user
}

func TestPasswordHashVerificationAndPolicy(t *testing.T) {
	hash, err := HashPassword("candidate-2026-safe")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "candidate-2026-safe" {
		t.Fatal("password stored in plaintext")
	}
	if err := VerifyPassword(hash, "candidate-2026-safe"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(VerifyPassword(hash, "wrong-password"), apperror.ErrUnauthenticated) {
		t.Fatal("wrong password should be unauthenticated")
	}
	for _, weak := range []string{"short1", "onlyletterslong", "123456789012345", ""} {
		if !errors.Is(func() error { _, err := HashPassword(weak); return err }(), apperror.ErrValidation) {
			t.Fatalf("weak password %q should fail policy", weak)
		}
	}
}

func TestLoginAuthenticateLogoutLifecycle(t *testing.T) {
	_, service, _, user := authFixture(t)
	ctx := context.Background()
	login, err := service.Login(ctx, " BREEDER@EXAMPLE.TEST ", "strong-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	if login.Token == "" || login.User.ID != user.ID {
		t.Fatalf("login = %+v", login)
	}
	principal, err := service.Authenticate(ctx, login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != user.ID || principal.Role != domain.RoleBreeder || principal.InstitutionID != user.InstitutionID {
		t.Fatalf("principal = %+v", principal)
	}
	if err := service.Logout(ctx, login.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, login.Token); err != nil {
		t.Fatalf("logout idempotence: %v", err)
	}
	if !errors.Is(func() error { _, err := service.Authenticate(ctx, login.Token); return err }(), apperror.ErrUnauthenticated) {
		t.Fatal("revoked token should fail authentication")
	}
}

func TestExpiredSessionIsRejectedAtExactBoundary(t *testing.T) {
	_, service, manual, _ := authFixture(t)
	login, err := service.Login(context.Background(), "breeder@example.test", "strong-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	manual.Set(login.ExpiresAt.Add(-time.Nanosecond))
	if _, err := service.Authenticate(context.Background(), login.Token); err != nil {
		t.Fatalf("before expiry: %v", err)
	}
	manual.Set(login.ExpiresAt)
	if !errors.Is(func() error { _, err := service.Authenticate(context.Background(), login.Token); return err }(), apperror.ErrUnauthenticated) {
		t.Fatal("expiry boundary must reject session")
	}
}

func TestLoginDoesNotRevealUnknownAccountOrWrongPassword(t *testing.T) {
	_, service, _, _ := authFixture(t)
	_, missingErr := service.Login(context.Background(), "missing@example.test", "strong-password-2026")
	_, wrongErr := service.Login(context.Background(), "breeder@example.test", "wrong-password")
	if !errors.Is(missingErr, apperror.ErrUnauthenticated) || !errors.Is(wrongErr, apperror.ErrUnauthenticated) {
		t.Fatalf("missing=%v wrong=%v", missingErr, wrongErr)
	}
}

func TestAuthenticationHonorsContextCancellation(t *testing.T) {
	_, service, _, _ := authFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Login(ctx, "breeder@example.test", "strong-password-2026"); !errors.Is(err, context.Canceled) {
		t.Fatalf("login error = %v", err)
	}
	if _, err := service.Authenticate(ctx, "token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("authenticate error = %v", err)
	}
}
