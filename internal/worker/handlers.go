package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type BusinessHandlers struct {
	Store  *store.DB
	Logger *slog.Logger
}

func (b BusinessHandlers) Map() map[domain.JobType]Handler {
	return map[domain.JobType]Handler{
		domain.JobObservationReminder: HandlerFunc(b.observationReminder),
		domain.JobAnomalyReview:       HandlerFunc(b.anomalyReview),
		domain.JobSeasonSummary:       HandlerFunc(b.seasonSummary),
		domain.JobAdoptionFollowUp:    HandlerFunc(b.adoptionFollowUp),
	}
}

func (b BusinessHandlers) observationReminder(ctx context.Context, job domain.WorkerJob) error {
	batch, err := b.Store.GetObservationBatch(ctx, nil, job.ObjectID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return fmt.Errorf("reminder batch removed: %w", apperror.ErrPermanent)
		}
		return err
	}
	if batch.Status != domain.ObservationOpen {
		return nil
	}
	b.log().InfoContext(ctx, "observation reminder recorded", "batch_id", batch.ID, "window", batch.WindowName)
	return nil
}

func (b BusinessHandlers) anomalyReview(ctx context.Context, job domain.WorkerJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	count, err := b.Store.CountAnomalies(ctx, nil, job.ObjectID)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var payload struct {
		AnomalyCount int `json:"anomaly_count"`
	}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("invalid anomaly payload: %w: %w", err, apperror.ErrPermanent)
	}
	if payload.AnomalyCount != count {
		return fmt.Errorf("anomaly count changed from %d to %d", payload.AnomalyCount, count)
	}
	b.log().InfoContext(ctx, "anomaly review evidence prepared", "batch_id", job.ObjectID, "anomalies", count)
	return nil
}

func (b BusinessHandlers) seasonSummary(ctx context.Context, job domain.WorkerJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if job.ObjectType != "season" || job.ObjectID == "" {
		return fmt.Errorf("season summary target is malformed: %w", apperror.ErrPermanent)
	}
	b.log().InfoContext(ctx, "season summary completed", "season", job.ObjectID)
	return nil
}

func (b BusinessHandlers) adoptionFollowUp(ctx context.Context, job domain.WorkerJob) error {
	adoption, err := b.Store.GetAdoption(ctx, nil, job.ObjectID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return fmt.Errorf("follow-up adoption removed: %w", apperror.ErrPermanent)
		}
		return err
	}
	if adoption.Status == domain.AdoptionRevoked {
		b.log().InfoContext(ctx, "adoption follow-up closed after revocation", "adoption_id", adoption.ID)
		return nil
	}
	b.log().InfoContext(ctx, "adoption effectiveness follow-up recorded", "adoption_id", adoption.ID, "region", adoption.Region)
	return nil
}

func (b BusinessHandlers) log() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.Default()
}
