package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type Role string

const (
	RoleStationStaff    Role = "station_staff"
	RoleBreeder         Role = "breeder"
	RoleReviewExpert    Role = "review_expert"
	RoleSeedCustodian   Role = "seed_custodian"
	RoleRegionalPlanner Role = "regional_planner"
	RoleAdmin           Role = "admin"
)

var knownRoles = map[Role]struct{}{
	RoleStationStaff: {}, RoleBreeder: {}, RoleReviewExpert: {},
	RoleSeedCustodian: {}, RoleRegionalPlanner: {}, RoleAdmin: {},
}

func (r Role) Valid() bool {
	_, ok := knownRoles[r]
	return ok
}

func (r Role) Can(permission Permission) bool {
	if r == RoleAdmin {
		return true
	}
	allowed := permissionsByRole[r]
	for _, candidate := range allowed {
		if candidate == permission {
			return true
		}
	}
	return false
}

type Permission string

const (
	PermissionSubmitApplication   Permission = "application.submit"
	PermissionVerifyQualification Permission = "application.qualify"
	PermissionApprovePlan         Permission = "plan.approve"
	PermissionAllocateSeed        Permission = "allocation.seed"
	PermissionAllocatePlot        Permission = "allocation.plot"
	PermissionRecordObservation   Permission = "observation.record"
	PermissionLockData            Permission = "observation.lock"
	PermissionReviewTrial         Permission = "review.submit"
	PermissionPublishConclusion   Permission = "conclusion.publish"
	PermissionAdoptRegion         Permission = "adoption.create"
	PermissionRevokeAdoption      Permission = "adoption.revoke"
)

var permissionsByRole = map[Role][]Permission{
	RoleBreeder:         {PermissionSubmitApplication},
	RoleStationStaff:    {PermissionVerifyQualification, PermissionApprovePlan, PermissionRecordObservation, PermissionLockData},
	RoleSeedCustodian:   {PermissionAllocateSeed, PermissionAllocatePlot},
	RoleReviewExpert:    {PermissionReviewTrial, PermissionPublishConclusion},
	RoleRegionalPlanner: {PermissionAdoptRegion, PermissionRevokeAdoption},
}

type Institution struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID            string    `json:"id"`
	InstitutionID string    `json:"institution_id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	Role          Role      `json:"role"`
	Region        string    `json:"region"`
	Active        bool      `json:"active"`
	PasswordHash  string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (u User) Validate() error {
	if strings.TrimSpace(u.ID) == "" || strings.TrimSpace(u.InstitutionID) == "" {
		return fmt.Errorf("user identity: %w", apperror.ErrValidation)
	}
	if !strings.Contains(u.Email, "@") {
		return fmt.Errorf("user email: %w", apperror.ErrValidation)
	}
	if !u.Role.Valid() {
		return fmt.Errorf("user role %q: %w", u.Role, apperror.ErrValidation)
	}
	if strings.TrimSpace(u.Region) == "" {
		return fmt.Errorf("user region: %w", apperror.ErrValidation)
	}
	return nil
}

func (u User) Require(permission Permission) error {
	if !u.Active {
		return fmt.Errorf("inactive user: %w", apperror.ErrForbidden)
	}
	if !u.Role.Can(permission) {
		return fmt.Errorf("role %s lacks %s: %w", u.Role, permission, apperror.ErrForbidden)
	}
	return nil
}

type Session struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s Session) UsableAt(now time.Time) error {
	if s.RevokedAt != nil {
		return fmt.Errorf("session revoked: %w", apperror.ErrUnauthenticated)
	}
	if !now.Before(s.ExpiresAt) {
		return fmt.Errorf("session expired: %w", apperror.ErrExpired)
	}
	return nil
}

type Principal struct {
	UserID        string `json:"user_id"`
	InstitutionID string `json:"institution_id"`
	Region        string `json:"region"`
	Role          Role   `json:"role"`
	SessionID     string `json:"session_id"`
}

func (p Principal) Require(permission Permission) error {
	if p.UserID == "" || p.InstitutionID == "" {
		return apperror.ErrUnauthenticated
	}
	if !p.Role.Can(permission) {
		return fmt.Errorf("principal role %s: %w", p.Role, apperror.ErrForbidden)
	}
	return nil
}

func (p Principal) RequireRegion(region string) error {
	if p.Role == RoleAdmin {
		return nil
	}
	if p.Region != region {
		return fmt.Errorf("region %s is outside %s: %w", region, p.Region, apperror.ErrForbidden)
	}
	return nil
}

func RequireSeparation(actor Principal, ownerUserID, ownerInstitutionID string) error {
	if actor.Role == RoleAdmin {
		return nil
	}
	if actor.UserID == ownerUserID {
		return fmt.Errorf("actor cannot approve own submission: %w", apperror.ErrForbidden)
	}
	if actor.Role == RoleReviewExpert && actor.InstitutionID == ownerInstitutionID {
		return fmt.Errorf("expert institution conflicts with applicant: %w", apperror.ErrForbidden)
	}
	return nil
}
