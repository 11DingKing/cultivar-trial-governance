package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type serviceFixture struct {
	db        *store.DB
	service   Service
	clock     *clock.Manual
	breeder   domain.Principal
	staff     domain.Principal
	custodian domain.Principal
	expertA   domain.Principal
	expertB   domain.Principal
	planner   domain.Principal
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	db, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	ids := idgen.NewSequence(1)
	service := Service{Store: db, Clock: manual, IDs: ids, Audit: audit.Factory{IDs: ids}, WorkerMaxAttempts: 4}
	fixture := &serviceFixture{db: db, service: service, clock: manual}
	fixture.breeder = fixture.addUser(t, "breeder", "breeding", domain.RoleBreeder, "north")
	fixture.staff = fixture.addUser(t, "staff", "station", domain.RoleStationStaff, "north")
	fixture.custodian = fixture.addUser(t, "custodian", "seed-bank", domain.RoleSeedCustodian, "north")
	fixture.expertA = fixture.addUser(t, "expert-a", "expert-center-a", domain.RoleReviewExpert, "north")
	fixture.expertB = fixture.addUser(t, "expert-b", "expert-center-b", domain.RoleReviewExpert, "north")
	fixture.planner = fixture.addUser(t, "planner", "regional-office", domain.RoleRegionalPlanner, "north")
	return fixture
}

func (f *serviceFixture) addUser(t *testing.T, suffix, institutionName string, role domain.Role, region string) domain.Principal {
	t.Helper()
	now := f.clock.Now()
	institutionID := "institution-" + suffix
	userID := "user-" + suffix
	institution := domain.Institution{ID: institutionID, Name: institutionName, Region: region, Active: true, CreatedAt: now}
	user := domain.User{
		ID: userID, InstitutionID: institutionID, Email: suffix + "@example.test", DisplayName: suffix,
		Role: role, Region: region, Active: true, PasswordHash: "hash", CreatedAt: now, UpdatedAt: now,
	}
	if err := f.db.InsertInstitution(context.Background(), nil, institution); err != nil {
		t.Fatal(err)
	}
	if err := f.db.InsertUser(context.Background(), nil, user); err != nil {
		t.Fatal(err)
	}
	return domain.Principal{UserID: userID, InstitutionID: institutionID, Region: region, Role: role, SessionID: "session-" + suffix}
}

func (f *serviceFixture) submit(t *testing.T, key string) SubmitApplicationResult {
	t.Helper()
	result, err := f.service.SubmitApplication(audit.WithRequestID(context.Background(), "request-submit"), f.breeder, SubmitApplicationInput{
		VarietyCode: "CV-" + key, VarietyName: "候选品种" + key, Crop: "maize", Generation: 2,
		TraitsJSON: `{"drought":"medium"}`, Region: "north", PolicyRef: "POLICY-SUBMIT",
		SubmissionNote: "申请在北部区域开展完整的多站点候选品种试验", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("submit application: %v", err)
	}
	return result
}

func (f *serviceFixture) qualifyAndPlan(t *testing.T, applicationID string) domain.TrialPlan {
	t.Helper()
	ctx := audit.WithRequestID(context.Background(), "request-approve")
	if _, err := f.service.QualifyApplication(ctx, f.staff, QualificationInput{
		ApplicationID: applicationID, Approved: true, Note: "资质文件与机构登记信息已经核验一致", PolicyRef: "POLICY-QUALIFY",
	}); err != nil {
		t.Fatalf("qualify: %v", err)
	}
	plan, err := f.service.ApprovePlan(ctx, f.staff, ApprovePlanInput{
		ApplicationID: applicationID, Season: "2026-summer",
		ObservationOpensAt: f.clock.Now().Add(time.Hour), ObservationClosesAt: f.clock.Now().Add(60 * 24 * time.Hour),
		RequiredReviewers: 2, MaxReviewers: 3, PolicyRef: "POLICY-PLAN",
	})
	if err != nil {
		t.Fatalf("approve plan: %v", err)
	}
	return plan
}

func (f *serviceFixture) seedResources(t *testing.T, varietyID, suffix string) (domain.SeedLot, domain.PlotSeason) {
	t.Helper()
	now := f.clock.Now()
	seed := domain.SeedLot{
		ID: "seed-" + suffix, VarietyID: varietyID, InstitutionID: f.custodian.InstitutionID,
		LotCode: "LOT-" + suffix, QuantityGrams: 1000, ExpiresAt: now.Add(180 * 24 * time.Hour), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	site := domain.TrialSite{
		ID: "site-" + suffix, InstitutionID: f.staff.InstitutionID, Code: "SITE-" + suffix,
		Name: "试验站点" + suffix, Region: "north", Timezone: "Asia/Shanghai", Active: true, CreatedAt: now,
	}
	plot := domain.PlotSeason{
		ID: "plot-" + suffix, SiteID: site.ID, PlotCode: "PLOT-" + suffix, Season: "2026-summer",
		AreaSquareM: 200, Status: domain.PlotSeasonAvailable, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := f.db.InsertSeedLot(context.Background(), nil, seed); err != nil {
		t.Fatal(err)
	}
	if err := f.db.InsertTrialSite(context.Background(), nil, site); err != nil {
		t.Fatal(err)
	}
	if err := f.db.InsertPlotSeason(context.Background(), nil, plot); err != nil {
		t.Fatal(err)
	}
	return seed, plot
}

func TestSubmitApplicationIsIdempotentAndAudited(t *testing.T) {
	f := newServiceFixture(t)
	first := f.submit(t, "idempotency-0001")
	second := f.submit(t, "idempotency-0001")
	if first.Application.ID != second.Application.ID || first.Variety.ID != second.Variety.ID {
		t.Fatalf("idempotent replay created new objects: first=%+v second=%+v", first, second)
	}
	values, total, err := f.db.ListApplications(context.Background(), store.ApplicationFilter{Region: "north", Page: store.Page{Limit: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(values) != 1 {
		t.Fatalf("applications total=%d len=%d", total, len(values))
	}
	audits, err := f.db.ListAuditForObject(context.Background(), "application", first.Application.ID, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].Action != "application.submit" || audits[0].RequestID != "request-submit" {
		t.Fatalf("unexpected audits: %+v", audits)
	}
}

func TestSubmitApplicationRejectsKeyReuseWithDifferentPayload(t *testing.T) {
	f := newServiceFixture(t)
	f.submit(t, "idempotency-0002")
	_, err := f.service.SubmitApplication(context.Background(), f.breeder, SubmitApplicationInput{
		VarietyCode: "DIFFERENT", VarietyName: "不同品种", Crop: "rice", Generation: 1,
		Region: "north", PolicyRef: "POLICY-SUBMIT", SubmissionNote: "不同的区域试验申请说明内容",
		IdempotencyKey: "idempotency-0002",
	})
	if !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
}

func TestQualificationEnforcesDutySeparationAndRegion(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "qualification-1")
	_, err := f.service.QualifyApplication(context.Background(), f.breeder, QualificationInput{
		ApplicationID: result.Application.ID, Approved: true, Note: "申请人试图自行完成资质核验操作", PolicyRef: "POLICY",
	})
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("breeder qualify error = %v", err)
	}
	foreign := f.addUser(t, "south-staff", "south-station", domain.RoleStationStaff, "south")
	_, err = f.service.QualifyApplication(context.Background(), foreign, QualificationInput{
		ApplicationID: result.Application.ID, Approved: true, Note: "跨区域人员试图完成资质核验操作", PolicyRef: "POLICY",
	})
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("cross-region error = %v", err)
	}
	qualified, err := f.service.QualifyApplication(context.Background(), f.staff, QualificationInput{
		ApplicationID: result.Application.ID, Approved: true, Note: "本区域独立人员已核验所有资质材料", PolicyRef: "POLICY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if qualified.Status != domain.ApplicationQualified {
		t.Fatalf("status = %s", qualified.Status)
	}
}

func TestPlanApprovalRollsBackWhenPlanConstraintFails(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "plan-rollback")
	if _, err := f.service.QualifyApplication(context.Background(), f.staff, QualificationInput{
		ApplicationID: result.Application.ID, Approved: true, Note: "核验材料完整并且符合受理政策要求", PolicyRef: "POLICY",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.ApprovePlan(context.Background(), f.staff, ApprovePlanInput{
		ApplicationID: result.Application.ID, Season: "2026", ObservationOpensAt: f.clock.Now(),
		ObservationClosesAt: f.clock.Now(), RequiredReviewers: 1, MaxReviewers: 1, PolicyRef: "POLICY",
	})
	if !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("error = %v", err)
	}
	application, err := f.db.GetApplication(context.Background(), nil, result.Application.ID)
	if err != nil {
		t.Fatal(err)
	}
	if application.Status != domain.ApplicationQualified || application.Version != 2 {
		t.Fatalf("application changed despite rollback: %+v", application)
	}
	if _, err := f.db.GetTrialPlanByApplication(context.Background(), nil, application.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("invalid plan should not persist: %v", err)
	}
}

func TestResourceAllocationChangesAllEntitiesAtomically(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "allocation-happy")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "happy")
	allocation, err := f.service.AllocateResources(audit.WithRequestID(context.Background(), "allocate-request"), f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 300, PolicyRef: "POLICY-ALLOCATE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocation.SeedGrams != 300 || allocation.Status != "reserved" {
		t.Fatalf("allocation = %+v", allocation)
	}
	gotSeed, err := f.db.GetSeedLot(context.Background(), nil, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotPlot, err := f.db.GetPlotSeason(context.Background(), nil, plot.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotApplication, err := f.db.GetApplication(context.Background(), nil, result.Application.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSeed.ReservedGrams != 300 || gotPlot.ApplicationID != result.Application.ID || gotPlot.Status != domain.PlotSeasonReserved || gotApplication.Status != domain.ApplicationAllocated {
		t.Fatalf("atomic state mismatch: seed=%+v plot=%+v application=%+v", gotSeed, gotPlot, gotApplication)
	}
	audits, err := f.db.ListAuditForObject(context.Background(), "application", result.Application.ID, store.Page{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 2 {
		t.Fatalf("expected submit and allocation audit: %+v", audits)
	}
}

func TestResourceAllocationRollsBackSeedWhenPlotConflicts(t *testing.T) {
	f := newServiceFixture(t)
	first := f.submit(t, "allocation-first")
	second := f.submit(t, "allocation-second")
	f.qualifyAndPlan(t, first.Application.ID)
	f.qualifyAndPlan(t, second.Application.ID)
	seedFirst, plot := f.seedResources(t, first.Variety.ID, "shared")
	seedSecond, _ := f.seedResources(t, second.Variety.ID, "second")
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: first.Application.ID, SeedLotID: seedFirst.ID, PlotSeasonID: plot.ID, SeedGrams: 200, PolicyRef: "POLICY",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: second.Application.ID, SeedLotID: seedSecond.ID, PlotSeasonID: plot.ID, SeedGrams: 250, PolicyRef: "POLICY",
	})
	if !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
	gotSeed, err := f.db.GetSeedLot(context.Background(), nil, seedSecond.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSeed.ReservedGrams != 0 || gotSeed.Version != 1 {
		t.Fatalf("seed reservation leaked: %+v", gotSeed)
	}
	gotApplication, err := f.db.GetApplication(context.Background(), nil, second.Application.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotApplication.Status != domain.ApplicationPlanApproved {
		t.Fatalf("application changed: %+v", gotApplication)
	}
}

func TestConcurrentAllocationOfOnePlotHasOneWinner(t *testing.T) {
	f := newServiceFixture(t)
	first := f.submit(t, "concurrent-a")
	second := f.submit(t, "concurrent-b")
	f.qualifyAndPlan(t, first.Application.ID)
	f.qualifyAndPlan(t, second.Application.ID)
	seedA, plot := f.seedResources(t, first.Variety.ID, "concurrent-a")
	seedB, _ := f.seedResources(t, second.Variety.ID, "concurrent-b")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, input := range []AllocateInput{
		{ApplicationID: first.Application.ID, SeedLotID: seedA.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P"},
		{ApplicationID: second.Application.ID, SeedLotID: seedB.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P"},
	} {
		input := input
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.service.AllocateResources(context.Background(), f.custodian, input)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var success, failed int
	for err := range results {
		if err == nil {
			success++
		} else {
			failed++
		}
	}
	if success != 1 || failed != 1 {
		t.Fatalf("success=%d failed=%d", success, failed)
	}
	gotPlot, err := f.db.GetPlotSeason(context.Background(), nil, plot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotPlot.ApplicationID != first.Application.ID && gotPlot.ApplicationID != second.Application.ID {
		t.Fatalf("owner = %s", gotPlot.ApplicationID)
	}
}

func TestCancelledContextLeavesAllocationUnchanged(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "context-cancel")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "context-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.service.AllocateResources(ctx, f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	gotSeed, _ := f.db.GetSeedLot(context.Background(), nil, seed.ID)
	gotPlot, _ := f.db.GetPlotSeason(context.Background(), nil, plot.ID)
	if gotSeed.ReservedGrams != 0 || gotPlot.ApplicationID != "" {
		t.Fatalf("state changed: seed=%+v plot=%+v", gotSeed, gotPlot)
	}
}

func TestObservationLockEnqueuesAnomalyReviewInSameTransaction(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "observation")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "observation")
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "P"); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(2 * time.Hour)
	batch, err := f.service.CreateObservationBatch(context.Background(), f.staff, CreateBatchInput{
		ApplicationID: result.Application.ID, WindowName: "seedling", OpensAt: f.clock.Now().Add(-time.Hour),
		ClosesAt: f.clock.Now().Add(24 * time.Hour), PolicyRef: "P",
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := f.service.RecordObservation(context.Background(), f.staff, RecordObservationInput{
		BatchID: batch.ID, Metric: "height", Value: 999, Unit: "cm", ObservedAt: f.clock.Now().Add(-time.Minute),
		Rule: domain.ObservationRule{Metric: "height", Unit: "cm", Min: 1, Max: 100}, PolicyRef: "P",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Anomalous {
		t.Fatal("expected anomalous observation")
	}
	locked, err := f.service.LockObservationBatch(context.Background(), f.staff, batch.ID, "P")
	if err != nil {
		t.Fatal(err)
	}
	if locked.Status != domain.ObservationLocked {
		t.Fatalf("status = %s", locked.Status)
	}
	job, err := f.db.ClaimJob(context.Background(), "test-worker", f.clock.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.Type != domain.JobAnomalyReview || job.ObjectID != batch.ID {
		t.Fatalf("job = %+v", job)
	}
}

func TestReviewPublishAdoptAndRevokeEndToEnd(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "governance-end-to-end")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "governance-end-to-end")
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID,
		SeedGrams: 150, PolicyRef: "POLICY-ALLOCATE",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "POLICY-START"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LockApplicationData(context.Background(), f.staff, result.Application.ID, "POLICY-LOCK"); err != nil {
		t.Fatal(err)
	}
	for index, expert := range []domain.Principal{f.expertA, f.expertB} {
		review, err := f.service.SubmitReview(audit.WithRequestID(context.Background(), fmt.Sprintf("review-%d", index)), expert, SubmitReviewInput{
			ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
			Rationale: fmt.Sprintf("专家%d核对多站点数据后确认候选品种表现达到区域采用标准", index+1),
			PolicyRef: "POLICY-REVIEW",
		})
		if err != nil {
			t.Fatal(err)
		}
		if review.ExpertUserID != expert.UserID {
			t.Fatalf("review expert = %s", review.ExpertUserID)
		}
	}
	conclusion, err := f.service.DraftConclusion(context.Background(), f.expertA, DraftConclusionInput{
		ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
		Summary:   "综合两个独立专家的复核意见与锁定观测数据，建议在北部区域按政策采用该品种",
		PolicyRef: "POLICY-CONCLUSION",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conclusion.Version != 1 || conclusion.Status != domain.ConclusionDraft {
		t.Fatalf("draft = %+v", conclusion)
	}
	published, err := f.service.PublishConclusion(context.Background(), f.expertA, conclusion.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != domain.ConclusionPublished || published.PublishedAt == nil {
		t.Fatalf("published = %+v", published)
	}
	adoption, err := f.service.AdoptConclusion(context.Background(), f.planner, AdoptInput{
		ConclusionID: conclusion.ID, InstitutionID: f.planner.InstitutionID,
		Region: "north", PolicyRef: "POLICY-ADOPT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if adoption.Status != domain.AdoptionActive {
		t.Fatalf("adoption = %+v", adoption)
	}
	revoked, err := f.service.RevokeAdoption(context.Background(), f.planner, adoption.ID,
		"区域政策更新后需要撤销采用并保留完整追溯记录", "POLICY-REVOKE")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Status != domain.AdoptionRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoked = %+v", revoked)
	}
	stored, err := f.db.GetAdoption(context.Background(), nil, adoption.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.AdoptionRevoked || stored.AdoptedBy != adoption.AdoptedBy || stored.RevokeReason == "" {
		t.Fatalf("stored adoption history = %+v", stored)
	}
}

// TestAdoptionRejectsAlreadyAdoptedApplication guards against a regression where a regional
// reviewer could re-issue the adoption flow for a conclusion whose application had already been
// adopted. The duplicate call must not create a second adoption record or push the candidate
// variety lifecycle forward again.
func TestAdoptionRejectsAlreadyAdoptedApplication(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "duplicate-adoption")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "duplicate-adoption")
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 150, PolicyRef: "POLICY-ALLOCATE",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "POLICY-START"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LockApplicationData(context.Background(), f.staff, result.Application.ID, "POLICY-LOCK"); err != nil {
		t.Fatal(err)
	}
	for index, expert := range []domain.Principal{f.expertA, f.expertB} {
		if _, err := f.service.SubmitReview(audit.WithRequestID(context.Background(), fmt.Sprintf("review-%d", index)), expert, SubmitReviewInput{
			ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
			Rationale:     "专家核对多站点数据后确认候选品种表现达到区域采用标准",
			PolicyRef:     "POLICY-REVIEW",
		}); err != nil {
			t.Fatal(err)
		}
	}
	conclusion, err := f.service.DraftConclusion(context.Background(), f.expertA, DraftConclusionInput{
		ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
		Summary:   "综合两个独立专家的复核意见与锁定观测数据，建议在北部区域按政策采用该品种",
		PolicyRef: "POLICY-CONCLUSION",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PublishConclusion(context.Background(), f.expertA, conclusion.ID); err != nil {
		t.Fatal(err)
	}
	adoptInput := AdoptInput{
		ConclusionID: conclusion.ID, InstitutionID: f.planner.InstitutionID,
		Region: "north", PolicyRef: "POLICY-ADOPT",
	}
	if _, err := f.service.AdoptConclusion(context.Background(), f.planner, adoptInput); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	// A second planner at a different institution re-issues the same adoption flow. This must be
	// rejected instead of creating a duplicate adoption record and re-advancing the lifecycle.
	otherPlanner := f.addUser(t, "planner-dup", "regional-office-dup", domain.RoleRegionalPlanner, "north")
	if _, err := f.service.AdoptConclusion(context.Background(), otherPlanner, AdoptInput{
		ConclusionID: conclusion.ID, InstitutionID: otherPlanner.InstitutionID,
		Region: "north", PolicyRef: "POLICY-ADOPT",
	}); !errors.Is(err, apperror.ErrInvalidState) {
		t.Fatalf("duplicate adoption error = %v, want ErrInvalidState", err)
	}
	application, err := f.db.GetApplication(context.Background(), nil, result.Application.ID)
	if err != nil {
		t.Fatal(err)
	}
	if application.Status != domain.ApplicationAdopted {
		t.Fatalf("application status = %s, want adopted", application.Status)
	}
}

func TestReviewQuorumAndInstitutionSeparation(t *testing.T) {
	f := newServiceFixture(t)
	result := f.submit(t, "review-separation")
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, "review-separation")
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "P"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LockApplicationData(context.Background(), f.staff, result.Application.ID, "P"); err != nil {
		t.Fatal(err)
	}
	sameInstitutionExpert := domain.Principal{
		UserID: "expert-conflicted", InstitutionID: f.breeder.InstitutionID,
		Region: "north", Role: domain.RoleReviewExpert,
	}
	if _, err := f.service.SubmitReview(context.Background(), sameInstitutionExpert, SubmitReviewInput{
		ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
		Rationale: "同一申请机构专家不应被允许提交这一份复核意见", PolicyRef: "P",
	}); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("same-institution error = %v", err)
	}
	if _, err := f.service.SubmitReview(context.Background(), f.expertA, SubmitReviewInput{
		ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
		Rationale: "第一位独立专家确认锁定数据满足区域试验评价要求", PolicyRef: "P",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DraftConclusion(context.Background(), f.expertA, DraftConclusionInput{
		ApplicationID: result.Application.ID, Decision: domain.ReviewRecommend,
		Summary: "只有一份复核意见时不应形成正式的区域采用结论草案内容", PolicyRef: "P",
	}); !errors.Is(err, apperror.ErrCapacity) {
		t.Fatalf("quorum error = %v", err)
	}
}
