package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func testJob(id string, now time.Time) domain.WorkerJob {
	return domain.WorkerJob{
		ID: id, Type: domain.JobSeasonSummary, ObjectType: "season", ObjectID: "2026-summer",
		PayloadJSON: "{}", Status: domain.JobPending, Attempts: 0, MaxAttempts: 3,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func TestJobClaimCompleteLifecyclePersistsLeaseOwnership(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	job := testJob("job-complete", now)
	if err := db.InsertJob(ctx, nil, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJob(ctx, "worker-a", now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID || claimed.Status != domain.JobRunning || claimed.Attempts != 1 || claimed.LeaseOwner != "worker-a" {
		t.Fatalf("unexpected claimed job: %+v", claimed)
	}
	if claimed.LeaseExpiresAt == nil || !claimed.LeaseExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("lease expiry = %v", claimed.LeaseExpiresAt)
	}
	if err := db.CompleteJob(ctx, job.ID, "worker-b", now); !errors.Is(err, apperror.ErrStaleVersion) {
		t.Fatalf("foreign completion error = %v", err)
	}
	if err := db.CompleteJob(ctx, job.ID, "worker-a", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	completed, err := db.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.JobCompleted || completed.LeaseOwner != "" || completed.LeaseExpiresAt != nil || completed.LastError != "" {
		t.Fatalf("unexpected completed job: %+v", completed)
	}
}

func TestJobFailureSchedulesBackoffAndEventuallyDies(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	job := testJob("job-fail", now)
	job.MaxAttempts = 2
	if err := db.InsertJob(ctx, nil, job); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimJob(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FailJob(ctx, first, "worker", errors.New("temporary database outage"), false, now); err != nil {
		t.Fatal(err)
	}
	failed, err := db.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.JobFailed || failed.LastError == "" || !failed.AvailableAt.After(now) {
		t.Fatalf("unexpected failed job: %+v", failed)
	}
	if _, err := db.ClaimJob(ctx, "worker", now, time.Minute); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("job should not be claimable before backoff: %v", err)
	}
	second, err := db.ClaimJob(ctx, "worker", failed.AvailableAt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempts != 2 {
		t.Fatalf("attempts = %d", second.Attempts)
	}
	if err := db.FailJob(ctx, second, "worker", errors.New("still unavailable"), false, failed.AvailableAt); err != nil {
		t.Fatal(err)
	}
	dead, err := db.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != domain.JobDead {
		t.Fatalf("status = %s, want dead", dead.Status)
	}
}

func TestPermanentJobFailureSkipsRemainingAttempts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	job := testJob("job-permanent", now)
	job.MaxAttempts = 10
	if err := db.InsertJob(ctx, nil, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJob(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FailJob(ctx, claimed, "worker", apperror.ErrPermanent, true, now); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.JobDead || got.Attempts != 1 {
		t.Fatalf("unexpected permanent result: %+v", got)
	}
}

func TestExpiredWorkerLeaseReturnsJobToPending(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	job := testJob("job-recover", now)
	if err := db.InsertJob(ctx, nil, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJob(ctx, "crashed-worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != domain.JobRunning {
		t.Fatalf("status = %s", claimed.Status)
	}
	count, err := db.RecoverExpiredLeases(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered = %d, want 1", count)
	}
	recovered, err := db.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != domain.JobPending || recovered.LeaseOwner != "" || recovered.LeaseExpiresAt != nil {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
	if recovered.LastError == "" {
		t.Fatal("lease recovery should preserve diagnostic evidence")
	}
}

func TestClaimOrderingUsesAvailabilityThenID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	jobs := []domain.WorkerJob{
		testJob("job-z", now),
		testJob("job-a", now),
		testJob("job-future", now.Add(time.Hour)),
	}
	jobs[0].ObjectID = "season-z"
	jobs[1].ObjectID = "season-a"
	jobs[2].ObjectID = "season-future"
	for _, job := range jobs {
		if err := db.InsertJob(ctx, nil, job); err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.ClaimJob(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "job-a" {
		t.Fatalf("first job = %s, want job-a", first.ID)
	}
	if err := db.CompleteJob(ctx, first.ID, "worker", now); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimJob(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "job-z" {
		t.Fatalf("second job = %s, want job-z", second.ID)
	}
}

func TestAuditEventsRoundTripWithCorrelationAndEvidence(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := fixtureTime()
	for index := 0; index < 3; index++ {
		event := domain.AuditEvent{
			ID: fmt.Sprintf("audit-%d", index), ActorUserID: "actor", InstitutionID: "institution",
			RequestID: "request-42", Action: "application.change", ObjectType: "application",
			ObjectID: "application-1", Outcome: "success", PolicyRef: "POLICY-1",
			BeforeJSON: fmt.Sprintf(`{"version":%d}`, index), AfterJSON: fmt.Sprintf(`{"version":%d}`, index+1),
			CreatedAt: now.Add(time.Duration(index) * time.Minute),
		}
		if err := db.InsertAudit(ctx, nil, event); err != nil {
			t.Fatal(err)
		}
	}
	values, err := db.ListAuditForObject(ctx, "application", "application-1", Page{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("len = %d", len(values))
	}
	if !values[0].CreatedAt.After(values[1].CreatedAt) {
		t.Fatalf("events not newest first: %+v", values)
	}
	if values[0].RequestID != "request-42" || values[0].PolicyRef != "POLICY-1" {
		t.Fatalf("audit evidence lost: %+v", values[0])
	}
}

func TestIdempotencyRecordScopesMethodPathAndInstitution(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	institution, _ := seedIdentity(t, db, "idem", domain.RoleBreeder)
	now := fixtureTime()
	record := IdempotencyRecord{
		InstitutionID: institution.ID, Method: "POST", Path: "/v1/applications", Key: "same-key-123",
		RequestHash: "hash-a", ResponseCode: 201, ResponseJSON: `{"id":"a"}`,
		CreatedAt: formatTime(now), ExpiresAt: formatTime(now.Add(time.Hour)),
	}
	if err := db.InsertIdempotency(ctx, nil, record); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.GetIdempotency(ctx, nil, institution.ID, record.Method, record.Path, record.Key)
	if err != nil || !ok {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if got.RequestHash != record.RequestHash || got.ResponseJSON != record.ResponseJSON {
		t.Fatalf("round trip = %+v", got)
	}
	if _, ok, err := db.GetIdempotency(ctx, nil, institution.ID, "PUT", record.Path, record.Key); err != nil || ok {
		t.Fatalf("different method should miss: ok=%v err=%v", ok, err)
	}
	if err := db.InsertIdempotency(ctx, nil, record); err == nil {
		t.Fatal("duplicate idempotency scope should conflict")
	}
	count, err := db.PurgeIdempotency(ctx, formatTime(now.Add(time.Hour)))
	if err != nil || count != 1 {
		t.Fatalf("purged=%d err=%v", count, err)
	}
}
