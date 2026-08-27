package auth

import (
 "context"
 "errors"
 "testing"
 "github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

func TestSessionAtExpiryBoundaryIsRejected(t *testing.T) {
 _, service, manual, _ := authFixture(t); login, err := service.Login(context.Background(), "breeder@example.test", "strong-password-2026"); if err != nil { t.Fatal(err) }; manual.Set(login.ExpiresAt); if !errors.Is(func() error { _, err := service.Authenticate(context.Background(), " "+login.Token+" "); return err }(), apperror.ErrUnauthenticated) { t.Fatal("expiry boundary must reject session") }
}
