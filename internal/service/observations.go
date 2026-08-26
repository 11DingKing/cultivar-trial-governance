package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

type CreateBatchInput struct {
	ApplicationID string    `json:"-"`
	WindowName    string    `json:"window_name"`
	OpensAt       time.Time `json:"opens_at"`
	ClosesAt      time.Time `json:"closes_at"`
	PolicyRef     string    `json:"policy_ref"`
}

func (s Service) CreateObservationBatch(ctx context.Context, actor domain.Principal, input CreateBatchInput) (domain.ObservationBatch, error) {
	if err := actor.Require(domain.PermissionRecordObservation); err != nil {
		return domain.ObservationBatch{}, err
	}
	if !input.OpensAt.Before(input.ClosesAt) || strings.TrimSpace(input.WindowName) == "" {
		return domain.ObservationBatch{}, fmt.Errorf("observation batch window: %w", apperror.ErrValidation)
	}
	id, err := s.IDs.New("batch")
	if err != nil {
		return domain.ObservationBatch{}, err
	}
	now := s.Clock.Now()
	batch := domain.ObservationBatch{
		ID: id, ApplicationID: input.ApplicationID, WindowName: strings.TrimSpace(input.WindowName),
		OpensAt: input.OpensAt.UTC(), ClosesAt: input.ClosesAt.UTC(), Status: domain.ObservationOpen,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		application, err := s.Store.GetApplication(ctx, tx, input.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		if application.Status != domain.ApplicationRunning && application.Status != domain.ApplicationInterrupted {
			return fmt.Errorf("application %s cannot accept observation batches: %w", application.ID, apperror.ErrInvalidState)
		}
		plan, err := s.Store.GetTrialPlanByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		if batch.OpensAt.Before(plan.ObservationOpensAt) || batch.ClosesAt.After(plan.ObservationClosesAt.Add(24*time.Hour)) {
			return fmt.Errorf("batch window exceeds approved plan: %w", apperror.ErrConflict)
		}
		if err := s.Store.InsertObservationBatch(ctx, tx, batch); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "observation.batch.create", "observation_batch", batch.ID, input.PolicyRef, nil, batch)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return batch, err
}

type RecordObservationInput struct {
	BatchID    string                 `json:"-"`
	Metric     string                 `json:"metric"`
	Value      float64                `json:"value"`
	Unit       string                 `json:"unit"`
	ObservedAt time.Time              `json:"observed_at"`
	Rule       domain.ObservationRule `json:"-"`
	PolicyRef  string                 `json:"policy_ref"`
}

func (s Service) RecordObservation(ctx context.Context, actor domain.Principal, input RecordObservationInput) (domain.Observation, error) {
	if err := actor.Require(domain.PermissionRecordObservation); err != nil {
		return domain.Observation{}, err
	}
	id, err := s.IDs.New("observation")
	if err != nil {
		return domain.Observation{}, err
	}
	now := s.Clock.Now()
	var observation domain.Observation
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		batch, err := s.Store.GetObservationBatch(ctx, tx, input.BatchID)
		if err != nil {
			return err
		}
		application, err := s.Store.GetApplication(ctx, tx, batch.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		interrupted := application.Status == domain.ApplicationInterrupted
		if application.Status != domain.ApplicationRunning && !interrupted {
			return fmt.Errorf("application %s is %s: %w", application.ID, application.Status, apperror.ErrInvalidState)
		}
		recovery := true
		if application.Status == domain.ApplicationInterrupted {
			recovery = true
		}
		if err := batch.Accepts(now, recovery); err != nil {
			return err
		}
		if input.ObservedAt.Before(batch.OpensAt) || input.ObservedAt.After(now) {
			return fmt.Errorf("observed_at outside accepted period: %w", apperror.ErrValidation)
		}
		if input.Rule.Metric != input.Metric || input.Rule.Unit != input.Unit {
			return fmt.Errorf("observation rule does not match metric: %w", apperror.ErrValidation)
		}
		anomalous, err := input.Rule.Evaluate(input.Value)
		if err != nil {
			return err
		}
		observation = domain.Observation{
			ID: id, BatchID: batch.ID, Metric: strings.TrimSpace(input.Metric), Value: input.Value,
			Unit: strings.TrimSpace(input.Unit), ObservedAt: input.ObservedAt.UTC(), ReportedBy: actor.UserID,
			Anomalous: anomalous, CreatedAt: now,
		}
		if err := observation.Validate(); err != nil {
			return err
		}
		if err := s.Store.InsertObservation(ctx, tx, observation); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "observation.record", "observation", observation.ID, input.PolicyRef, nil, observation)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return observation, err
}

func (s Service) LockObservationBatch(ctx context.Context, actor domain.Principal, batchID, policyRef string) (domain.ObservationBatch, error) {
	if err := actor.Require(domain.PermissionLockData); err != nil {
		return domain.ObservationBatch{}, err
	}
	jobID, err := s.IDs.New("job")
	if err != nil {
		return domain.ObservationBatch{}, err
	}
	now := s.Clock.Now()
	var locked domain.ObservationBatch
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		batch, err := s.Store.GetObservationBatch(ctx, tx, batchID)
		if err != nil {
			return err
		}
		application, err := s.Store.GetApplication(ctx, tx, batch.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		locked, err = batch.Lock(now)
		if err != nil {
			return err
		}
		if err := s.Store.UpdateObservationBatch(ctx, tx, locked, batch.Version); err != nil {
			return err
		}
		anomalies, err := s.Store.CountAnomalies(ctx, tx, batch.ID)
		if err != nil {
			return err
		}
		if anomalies > 0 {
			job := domain.WorkerJob{
				ID: jobID, Type: domain.JobAnomalyReview, ObjectType: "observation_batch", ObjectID: batch.ID,
				PayloadJSON: fmt.Sprintf(`{"anomaly_count":%d}`, anomalies), Status: domain.JobPending,
				MaxAttempts: s.WorkerMaxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			}
			if err := s.Store.InsertJob(ctx, tx, job); err != nil {
				return err
			}
		}
		event, err := s.auditEvent(ctx, actor, "observation.lock", "observation_batch", batch.ID, policyRef, batch, locked)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return locked, err
}

func (s Service) LockApplicationData(ctx context.Context, actor domain.Principal, applicationID, policyRef string) (domain.Application, error) {
	if err := actor.Require(domain.PermissionLockData); err != nil {
		return domain.Application{}, err
	}
	var updated domain.Application
	err := s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		application, err := s.Store.GetApplication(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		updated, err = application.Transition(domain.ApplicationDataLocked, s.Clock.Now())
		if err != nil {
			return err
		}
		if err := s.Store.UpdateApplication(ctx, tx, updated, application.Version); err != nil {
			return err
		}
		plan, err := s.Store.GetTrialPlanByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		if err := s.Store.UpdateTrialPlanStatus(ctx, tx, plan.ID, domain.TrialPlanExecuting, domain.TrialPlanLocked, s.Clock.Now().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "application.data.lock", "application", application.ID, policyRef, application, updated)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return updated, err
}
