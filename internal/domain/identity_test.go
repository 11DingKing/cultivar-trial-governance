package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

func TestRolesExposeOnlyBusinessPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role    Role
		allowed Permission
		denied  Permission
	}{
		{RoleBreeder, PermissionSubmitApplication, PermissionPublishConclusion},
		{RoleStationStaff, PermissionApprovePlan, PermissionAllocateSeed},
		{RoleSeedCustodian, PermissionAllocateSeed, PermissionReviewTrial},
		{RoleReviewExpert, PermissionReviewTrial, PermissionAdoptRegion},
		{RoleRegionalPlanner, PermissionAdoptRegion, PermissionRecordObservation},
	}
	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			if !tc.role.Valid() {
				t.Fatalf("role %s should be valid", tc.role)
			}
			if !tc.role.Can(tc.allowed) {
				t.Fatalf("role %s should allow %s", tc.role, tc.allowed)
			}
			if tc.role.Can(tc.denied) {
				t.Fatalf("role %s should deny %s", tc.role, tc.denied)
			}
		})
	}
	if !RoleAdmin.Can(PermissionSubmitApplication) || !RoleAdmin.Can(PermissionRevokeAdoption) {
		t.Fatal("admin should possess every recognized permission")
	}
	if Role("unknown").Valid() || Role("unknown").Can(PermissionSubmitApplication) {
		t.Fatal("unknown role must have no authority")
	}
}

func TestPrincipalRequiresAuthenticationRoleAndRegion(t *testing.T) {
	t.Parallel()
	principal := Principal{UserID: "u", InstitutionID: "i", Region: "north", Role: RoleStationStaff}
	if err := principal.Require(PermissionApprovePlan); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(principal.Require(PermissionPublishConclusion), apperror.ErrForbidden) {
		t.Fatal("station staff should not publish conclusions")
	}
	if !errors.Is((Principal{}).Require(PermissionApprovePlan), apperror.ErrUnauthenticated) {
		t.Fatal("empty principal should be unauthenticated")
	}
	if err := principal.RequireRegion("north"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(principal.RequireRegion("south"), apperror.ErrForbidden) {
		t.Fatal("cross-region operation should be forbidden")
	}
	admin := Principal{UserID: "admin", InstitutionID: "system", Region: "national", Role: RoleAdmin}
	if err := admin.RequireRegion("any-region"); err != nil {
		t.Fatalf("admin region override: %v", err)
	}
}

func TestDutySeparationRejectsApplicantAndSameInstitutionExpert(t *testing.T) {
	t.Parallel()
	staff := Principal{UserID: "staff", InstitutionID: "station", Role: RoleStationStaff}
	if err := RequireSeparation(staff, "breeder", "breeding-center"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(RequireSeparation(staff, "staff", "breeding-center"), apperror.ErrForbidden) {
		t.Fatal("submitter cannot approve own application")
	}
	expert := Principal{UserID: "expert", InstitutionID: "breeding-center", Role: RoleReviewExpert}
	if !errors.Is(RequireSeparation(expert, "breeder", "breeding-center"), apperror.ErrForbidden) {
		t.Fatal("expert from applicant institution should be rejected")
	}
	admin := Principal{UserID: "admin", InstitutionID: "breeding-center", Role: RoleAdmin}
	if err := RequireSeparation(admin, "admin", "breeding-center"); err != nil {
		t.Fatalf("admin override should be explicit: %v", err)
	}
}

func TestUserValidationAndActiveAuthorization(t *testing.T) {
	t.Parallel()
	user := User{ID: "u", InstitutionID: "i", Email: "user@example.test", Role: RoleBreeder, Region: "north", Active: true}
	if err := user.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := user.Require(PermissionSubmitApplication); err != nil {
		t.Fatal(err)
	}
	inactive := user
	inactive.Active = false
	if !errors.Is(inactive.Require(PermissionSubmitApplication), apperror.ErrForbidden) {
		t.Fatal("inactive account should be forbidden")
	}
	for _, invalid := range []User{
		{InstitutionID: "i", Email: user.Email, Role: user.Role, Region: user.Region},
		{ID: "u", Email: user.Email, Role: user.Role, Region: user.Region},
		{ID: "u", InstitutionID: "i", Email: "invalid", Role: user.Role, Region: user.Region},
		{ID: "u", InstitutionID: "i", Email: user.Email, Role: "unknown", Region: user.Region},
		{ID: "u", InstitutionID: "i", Email: user.Email, Role: user.Role},
	} {
		if !errors.Is(invalid.Validate(), apperror.ErrValidation) {
			t.Fatalf("expected invalid user: %+v", invalid)
		}
	}
}

func TestSessionUsesStrictExpiryAndRevocation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	session := Session{ID: "s", ExpiresAt: now.Add(time.Hour)}
	if err := session.UsableAt(now); err != nil {
		t.Fatal(err)
	}
	if err := session.UsableAt(session.ExpiresAt.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(session.UsableAt(session.ExpiresAt), apperror.ErrExpired) {
		t.Fatal("expiry instant must be unusable")
	}
	revokedAt := now
	session.RevokedAt = &revokedAt
	if !errors.Is(session.UsableAt(now), apperror.ErrUnauthenticated) {
		t.Fatal("revoked session must be unauthenticated")
	}
}
