package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

func workerFixture(t *testing.T) (*store.DB, *clock.Manual, *Runner) {
	t.Helper()
	db, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	manual := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runner := &Runner{
		Store: db, Clock: manual, Owner: "worker-test", PollInterval: time.Millisecond,
		Lease: time.Second, JobTimeout: 100 * time.Millisecond, Handlers: map[domain.JobType]Handler{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return db, manual, runner
}

func insertWorkerJob(t *testing.T, db *store.DB, now time.Time, id string, jobType domain.JobType) {
	t.Helper()
	job := domain.WorkerJob{
		ID: id, Type: jobType, ObjectType: "season", ObjectID: id, PayloadJSON: "{}",
		Status: domain.JobPending, MaxAttempts: 3, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertJob(context.Background(), nil, job); err != nil {
		t.Fatal(err)
	}
}

func TestRunOneCompletesSuccessfullyHandledJob(t *testing.T) {
	db, manual, runner := workerFixture(t)
	insertWorkerJob(t, db, manual.Now(), "job-success", domain.JobSeasonSummary)
	var calls atomic.Int32
	runner.Handlers[domain.JobSeasonSummary] = HandlerFunc(func(ctx context.Context, job domain.WorkerJob) error {
		calls.Add(1)
		if job.ID != "job-success" || job.Attempts != 1 {
			t.Fatalf("job = %+v", job)
		}
		return nil
	})
	if err := runner.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	job, err := db.GetJob(context.Background(), "job-success")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobCompleted {
		t.Fatalf("status = %s", job.Status)
	}
}

func TestRunOneRetriesTransientFailure(t *testing.T) {
	db, manual, runner := workerFixture(t)
	insertWorkerJob(t, db, manual.Now(), "job-retry", domain.JobSeasonSummary)
	runner.Handlers[domain.JobSeasonSummary] = HandlerFunc(func(context.Context, domain.WorkerJob) error {
		return errors.New("temporary failure")
	})
	err := runner.RunOne(context.Background())
	if err == nil {
		t.Fatal("transient failure should be returned")
	}
	job, getErr := db.GetJob(context.Background(), "job-retry")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if job.Status != domain.JobFailed || job.Attempts != 1 || !job.AvailableAt.After(manual.Now()) {
		t.Fatalf("job = %+v", job)
	}
}

func TestRunOneMarksUnknownHandlerPermanent(t *testing.T) {
	db, manual, runner := workerFixture(t)
	insertWorkerJob(t, db, manual.Now(), "job-unknown", domain.JobAdoptionFollowUp)
	if err := runner.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := db.GetJob(context.Background(), "job-unknown")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobDead {
		t.Fatalf("status = %s", job.Status)
	}
}

func TestRunOnePropagatesHandlerDeadlineAndSchedulesRetry(t *testing.T) {
	db, manual, runner := workerFixture(t)
	runner.JobTimeout = 10 * time.Millisecond
	insertWorkerJob(t, db, manual.Now(), "job-timeout", domain.JobSeasonSummary)
	runner.Handlers[domain.JobSeasonSummary] = HandlerFunc(func(ctx context.Context, _ domain.WorkerJob) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err := runner.RunOne(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	job, err := db.GetJob(context.Background(), "job-timeout")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed {
		t.Fatalf("status = %s", job.Status)
	}
}

func TestRunnerStopsPromptlyWhenContextIsCancelled(t *testing.T) {
	_, _, runner := workerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerValidationRejectsUnsafeLeaseConfiguration(t *testing.T) {
	_, _, runner := workerFixture(t)
	runner.Lease = runner.PollInterval
	if err := runner.RunOne(context.Background()); err == nil {
		t.Fatal("lease equal to poll should fail")
	}
	runner.Lease = time.Second
	runner.Owner = ""
	if err := runner.RunOne(context.Background()); err == nil {
		t.Fatal("missing owner should fail")
	}
}

func TestBusinessHandlerRejectsMalformedSeasonTargetPermanently(t *testing.T) {
	db, _, _ := workerFixture(t)
	handlers := BusinessHandlers{Store: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	err := handlers.seasonSummary(context.Background(), domain.WorkerJob{ObjectType: "application", ObjectID: ""})
	if err == nil {
		t.Fatal("malformed season target should fail")
	}
	if !errors.Is(err, apperror.ErrPermanent) {
		t.Fatalf("error = %v, want permanent", err)
	}
}
