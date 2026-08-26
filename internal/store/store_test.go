package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/migrate"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), "file:"+path)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func fixtureTime() time.Time {
	return time.Date(2026, 2, 3, 4, 5, 6, 123456789, time.UTC)
}

func seedIdentity(t *testing.T, db *DB, suffix string, role domain.Role) (domain.Institution, domain.User) {
	t.Helper()
	now := fixtureTime()
	institution := domain.Institution{ID: "institution_" + suffix, Name: "机构-" + suffix, Region: "north", Active: true, CreatedAt: now}
	user := domain.User{
		ID: "user_" + suffix, InstitutionID: institution.ID, Email: suffix + "@example.test",
		DisplayName: "用户-" + suffix, Role: role, Region: "north", Active: true,
		PasswordHash: "hash", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertInstitution(context.Background(), nil, institution); err != nil {
		t.Fatalf("insert institution: %v", err)
	}
	if err := db.InsertUser(context.Background(), nil, user); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return institution, user
}

func seedApplication(t *testing.T, db *DB, suffix string) (domain.Variety, domain.Application, domain.User) {
	t.Helper()
	institution, user := seedIdentity(t, db, suffix, domain.RoleBreeder)
	now := fixtureTime()
	variety := domain.Variety{
		ID: "variety_" + suffix, OwnerInstitutionID: institution.ID, Code: "CODE-" + suffix,
		Name: "品种-" + suffix, Crop: "maize", Generation: 2, TraitsJSON: "{}", CreatedAt: now,
	}
	application := domain.Application{
		ID: "application_" + suffix, VarietyID: variety.ID, ApplicantUserID: user.ID,
		ApplicantInstitutionID: institution.ID, Region: "north", Status: domain.ApplicationSubmitted,
		PolicyRef: "POLICY-1", SubmissionNote: "提交完整的候选品种区域试验说明", Version: 1,
		SubmittedAt: now, UpdatedAt: now,
	}
	if err := db.InsertVariety(context.Background(), nil, variety); err != nil {
		t.Fatalf("insert variety: %v", err)
	}
	if err := db.InsertApplication(context.Background(), nil, application); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	return variety, application, user
}

func TestOpenAppliesVersionedMigrationsAndPragmas(t *testing.T) {
	db := openTestDB(t)
	version, err := migrate.CurrentVersion(context.Background(), db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}
	var foreignKeys int
	if err := db.SQL().QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var journal string
	if err := db.SQL().QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" {
		t.Fatalf("journal mode = %q, want wal", journal)
	}
	if err := migrate.Apply(context.Background(), db.SQL()); err != nil {
		t.Fatalf("repeat migration should be idempotent: %v", err)
	}
}

func TestDatabaseCanCloseAndRecoverPersistedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	first, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	now := fixtureTime()
	institution := domain.Institution{ID: "persistent", Name: "持久化机构", Region: "west", Active: true, CreatedAt: now}
	if err := first.InsertInstitution(ctx, nil, institution); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.GetInstitution(ctx, institution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != institution.Name || got.Region != institution.Region || !got.Active {
		t.Fatalf("recovered institution = %+v", got)
	}
}

func TestIdentitySessionRoundTripAndRevocation(t *testing.T) {
	db := openTestDB(t)
	_, user := seedIdentity(t, db, "session", domain.RoleStationStaff)
	now := fixtureTime()
	session := domain.Session{ID: "session_1", UserID: user.ID, TokenHash: "token-hash", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := db.InsertSession(context.Background(), nil, session); err != nil {
		t.Fatal(err)
	}
	gotSession, gotUser, err := db.FindSessionByHash(context.Background(), "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if gotSession.ID != session.ID || gotUser.ID != user.ID || gotUser.Role != user.Role {
		t.Fatalf("round trip mismatch: %+v %+v", gotSession, gotUser)
	}
	revokedAt := now.Add(time.Minute).Format(time.RFC3339Nano)
	if err := db.RevokeSession(context.Background(), session.ID, revokedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeSession(context.Background(), session.ID, revokedAt); err != nil {
		t.Fatalf("logout should be idempotent: %v", err)
	}
	gotSession, _, err = db.FindSessionByHash(context.Background(), "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if gotSession.RevokedAt == nil || gotSession.RevokedAt.Format(time.RFC3339Nano) != revokedAt {
		t.Fatalf("revoked session = %+v", gotSession)
	}
}

func TestForeignKeysRejectOrphanBusinessObjects(t *testing.T) {
	db := openTestDB(t)
	application := domain.Application{
		ID: "orphan", VarietyID: "missing", ApplicantUserID: "missing", ApplicantInstitutionID: "missing",
		Region: "north", Status: domain.ApplicationSubmitted, PolicyRef: "P",
		SubmissionNote: "足够长的申请业务说明内容", Version: 1, SubmittedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}
	if err := db.InsertApplication(context.Background(), nil, application); err == nil {
		t.Fatal("orphan application should violate foreign key")
	}
}

func TestApplicationPaginationFiltersAndStableOrdering(t *testing.T) {
	db := openTestDB(t)
	for index := 0; index < 7; index++ {
		suffix := fmt.Sprintf("page_%d", index)
		_, application, _ := seedApplication(t, db, suffix)
		if index%2 == 0 {
			updated, err := application.Transition(domain.ApplicationQualified, application.UpdatedAt.Add(time.Duration(index+1)*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateApplication(context.Background(), nil, updated, application.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	values, total, err := db.ListApplications(context.Background(), ApplicationFilter{Region: "north", Status: domain.ApplicationQualified, Page: Page{Limit: 2, Offset: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(values) != 2 {
		t.Fatalf("total=%d len=%d, want 4 and 2", total, len(values))
	}
	if values[0].Status != domain.ApplicationQualified || values[1].Status != domain.ApplicationQualified {
		t.Fatalf("unexpected status in results: %+v", values)
	}
	if values[0].ID == values[1].ID {
		t.Fatal("pagination returned duplicate application")
	}
}

func TestOptimisticApplicationUpdateRejectsStaleVersion(t *testing.T) {
	db := openTestDB(t)
	_, application, _ := seedApplication(t, db, "stale")
	updated, err := application.Transition(domain.ApplicationQualified, fixtureTime().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateApplication(context.Background(), nil, updated, application.Version); err != nil {
		t.Fatal(err)
	}
	stale := updated
	stale.QualificationNote = "并发覆盖"
	stale.Version++
	if err := db.UpdateApplication(context.Background(), nil, stale, application.Version); !errors.Is(err, apperror.ErrStaleVersion) {
		t.Fatalf("error = %v, want stale version", err)
	}
}

func TestWithTxRollsBackAllEntitiesWhenCallbackFails(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	institution := domain.Institution{ID: "rollback", Name: "回滚机构", Region: "north", Active: true, CreatedAt: now}
	sentinel := errors.New("forced audit failure")
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := db.InsertInstitution(ctx, tx, institution); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v", err)
	}
	if _, err := db.GetInstitution(ctx, institution.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("rolled back institution error = %v", err)
	}
}

func TestContextCancellationPreventsTransactionCallback(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := db.WithTx(ctx, func(*sql.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if called {
		t.Fatal("callback must not execute after cancellation")
	}
}

func TestWithTxPreservesContextErrorChainWhenCallbackCanceled(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cause := context.Canceled
	err := db.WithTx(ctx, func(*sql.Tx) error {
		cancel()
		return cause
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want chain to expose context.Canceled", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want chain to expose original cause", err)
	}
	if !strings.Contains(err.Error(), "transaction failed") {
		t.Fatalf("error = %q, want transaction context preserved", err.Error())
	}
}

func TestConcurrentPlotReservationHasSingleWinner(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_, applicationA, _ := seedApplication(t, db, "race_a")
	_, applicationB, _ := seedApplication(t, db, "race_b")
	station, _ := seedIdentity(t, db, "station", domain.RoleStationStaff)
	now := fixtureTime()
	site := domain.TrialSite{ID: "site", InstitutionID: station.ID, Code: "S1", Name: "站点一", Region: "north", Timezone: "Asia/Shanghai", Active: true, CreatedAt: now}
	plot := domain.PlotSeason{ID: "plot", SiteID: site.ID, PlotCode: "P1", Season: "2026", AreaSquareM: 100, Status: domain.PlotSeasonAvailable, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.InsertTrialSite(ctx, nil, site); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertPlotSeason(ctx, nil, plot); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, applicationID := range []string{applicationA.ID, applicationB.ID} {
		applicationID := applicationID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			current, err := db.GetPlotSeason(ctx, nil, plot.ID)
			if err != nil {
				results <- err
				return
			}
			reserved, err := current.Reserve(applicationID, now)
			if err != nil {
				results <- err
				return
			}
			results <- db.UpdatePlotSeason(ctx, nil, reserved, current.Version)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, failures int
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d", successes, failures)
	}
	got, err := db.GetPlotSeason(ctx, nil, plot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApplicationID != applicationA.ID && got.ApplicationID != applicationB.ID {
		t.Fatalf("unexpected reservation owner: %+v", got)
	}
}
