package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/auth"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

func EnsureAdmin(ctx context.Context, db *store.DB, email, password string, now time.Time) error {
	if _, err := db.FindUserByEmail(ctx, email); err == nil {
		return nil
	} else if !errors.Is(err, apperror.ErrNotFound) {
		return fmt.Errorf("find bootstrap admin: %w", err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("bootstrap admin password: %w", err)
	}
	institution := domain.Institution{ID: "institution_system", Name: "系统治理机构", Region: "national", Active: true, CreatedAt: now}
	user := domain.User{
		ID: "user_admin", InstitutionID: institution.ID, Email: email, DisplayName: "系统管理员",
		Role: domain.RoleAdmin, Region: "national", Active: true, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
	}
	return db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := db.InsertInstitution(ctx, tx, institution); err != nil {
			return err
		}
		return db.InsertUser(ctx, tx, user)
	})
}
