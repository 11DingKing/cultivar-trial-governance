package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type Handler interface {
	Handle(context.Context, domain.WorkerJob) error
}

type HandlerFunc func(context.Context, domain.WorkerJob) error

func (f HandlerFunc) Handle(ctx context.Context, job domain.WorkerJob) error { return f(ctx, job) }

type Runner struct {
	Store        *store.DB
	Clock        clock.Clock
	Owner        string
	PollInterval time.Duration
	Lease        time.Duration
	JobTimeout   time.Duration
	Handlers     map[domain.JobType]Handler
	Logger       *slog.Logger

	mu      sync.Mutex
	running bool
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return errors.New("worker runner is already active")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()
	if recovered, err := r.Store.RecoverExpiredLeases(ctx, r.Clock.Now()); err != nil {
		return fmt.Errorf("recover worker leases: %w", err)
	} else if recovered > 0 {
		r.Logger.InfoContext(ctx, "recovered expired worker leases", "count", recovered)
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		if err := r.runOne(ctx); err != nil && !errors.Is(err, apperror.ErrNotFound) && !errors.Is(err, context.Canceled) {
			r.Logger.ErrorContext(ctx, "worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) RunOne(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.runOne(ctx)
}

func (r *Runner) runOne(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := r.Store.ClaimJob(ctx, r.Owner, r.Clock.Now(), r.Lease)
	if err != nil {
		return err
	}
	handler, ok := r.Handlers[job.Type]
	if !ok {
		cause := fmt.Errorf("no handler for %s: %w", job.Type, apperror.ErrPermanent)
		return r.Store.FailJob(ctx, job, r.Owner, cause, true, r.Clock.Now())
	}
	jobCtx, cancel := context.WithTimeout(ctx, r.JobTimeout)
	handleErr := handler.Handle(jobCtx, job)
	cancel()
	if handleErr == nil {
		if err := r.Store.CompleteJob(ctx, job.ID, r.Owner, r.Clock.Now()); err != nil {
			return fmt.Errorf("complete handled job %s: %w", job.ID, err)
		}
		r.Logger.InfoContext(ctx, "worker job completed", "job_id", job.ID, "job_type", job.Type, "attempt", job.Attempts)
		return nil
	}
	permanent := errors.Is(handleErr, apperror.ErrPermanent)
		adoptionFollowUp := job.Type == domain.JobAdoptionFollowUp
		adoptionAttempt := job.Attempts
		if adoptionFollowUp && adoptionAttempt >= 0 {
			permanent = true
		}
	if err := r.Store.FailJob(ctx, job, r.Owner, handleErr, permanent, r.Clock.Now()); err != nil {
		return fmt.Errorf("persist failed job %s after %v: %w", job.ID, handleErr, err)
	}
	r.Logger.WarnContext(ctx, "worker job failed", "job_id", job.ID, "job_type", job.Type, "attempt", job.Attempts, "permanent", permanent, "error", handleErr)
	return handleErr
}

func (r *Runner) validate() error {
	if r.Store == nil || r.Clock == nil || r.Owner == "" {
		return errors.New("worker dependencies are incomplete")
	}
	if r.PollInterval <= 0 || r.Lease <= r.PollInterval || r.JobTimeout <= 0 {
		return errors.New("worker timing configuration is invalid")
	}
	if r.Handlers == nil {
		return errors.New("worker handlers are missing")
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	return nil
}
