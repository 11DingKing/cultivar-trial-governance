package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

type AllocateInput struct {
	ApplicationID string `json:"-"`
	SeedLotID     string `json:"seed_lot_id"`
	PlotSeasonID  string `json:"plot_season_id"`
	SeedGrams     int64  `json:"seed_grams"`
	PolicyRef     string `json:"policy_ref"`
}

func (s Service) AllocateResources(ctx context.Context, actor domain.Principal, input AllocateInput) (domain.Allocation, error) {
	if err := actor.Require(domain.PermissionAllocateSeed); err != nil {
		return domain.Allocation{}, err
	}
	if err := actor.Require(domain.PermissionAllocatePlot); err != nil {
		return domain.Allocation{}, err
	}
	if strings.TrimSpace(input.PolicyRef) == "" {
		return domain.Allocation{}, fmt.Errorf("allocation policy: %w", apperror.ErrValidation)
	}
	allocationID, err := s.IDs.New("allocation")
	if err != nil {
		return domain.Allocation{}, err
	}
	now := s.Clock.Now()
	var allocation domain.Allocation
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := checkContext(ctx, "allocate resources"); err != nil {
			return err
		}
		application, err := s.Store.GetApplication(ctx, tx, input.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		if application.Status != domain.ApplicationPlanApproved {
			return fmt.Errorf("application %s is %s: %w", application.ID, application.Status, apperror.ErrInvalidState)
		}
		plan, err := s.Store.GetTrialPlanByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		seed, err := s.Store.GetSeedLot(ctx, tx, input.SeedLotID)
		if err != nil {
			return err
		}
		if seed.VarietyID != application.VarietyID {
			return fmt.Errorf("seed lot belongs to a different variety: %w", apperror.ErrConflict)
		}
		plot, err := s.Store.GetPlotSeason(ctx, tx, input.PlotSeasonID)
		if err != nil {
			return err
		}
		site, err := s.Store.GetTrialSite(ctx, tx, plot.SiteID)
		if err != nil {
			return err
		}
		if site.Region != plan.Region || plot.Season != plan.Season {
			return fmt.Errorf("plot region or season differs from plan: %w", apperror.ErrConflict)
		}
		reservedSeed, err := seed.Reserve(input.SeedGrams, now)
		if err != nil {
			return err
		}
		reservedPlot, err := plot.Reserve(application.ID, now)
		if err != nil {
			return err
		}
		updatedApplication, err := application.Transition(domain.ApplicationAllocated, now)
		if err != nil {
			return err
		}
		allocation = domain.Allocation{
			ID: allocationID, ApplicationID: application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID,
			SeedGrams: input.SeedGrams, Status: "reserved", AllocatedBy: actor.UserID, AllocatedAt: now,
		}
		if err := allocation.Validate(); err != nil {
			return err
		}
		if err := s.Store.UpdateSeedReservation(ctx, tx, reservedSeed, seed.Version); err != nil {
			return err
		}
		if err := s.Store.UpdatePlotSeason(ctx, tx, reservedPlot, plot.Version); err != nil {
			return err
		}
		if err := s.Store.InsertAllocation(ctx, tx, allocation); err != nil {
			return err
		}
		if err := s.Store.UpdateApplication(ctx, tx, updatedApplication, application.Version); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "resources.allocate", "application", application.ID,
			input.PolicyRef, map[string]any{"seed": seed, "plot": plot}, map[string]any{"seed": reservedSeed, "plot": reservedPlot, "allocation": allocation})
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	if err != nil {
		return domain.Allocation{}, fmt.Errorf("allocate resources transaction: %w", err)
	}
	return allocation, nil
}

func (s Service) StartTrial(ctx context.Context, actor domain.Principal, applicationID, policyRef string) (domain.Application, error) {
	if err := actor.Require(domain.PermissionRecordObservation); err != nil {
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
		allocation, err := s.Store.GetAllocationByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		if allocation.Status != "reserved" {
			return fmt.Errorf("allocation %s is not reserved: %w", allocation.ID, apperror.ErrInvalidState)
		}
		updated, err = application.Transition(domain.ApplicationRunning, s.Clock.Now())
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
		if err := s.Store.UpdateTrialPlanStatus(ctx, tx, plan.ID, domain.TrialPlanApproved, domain.TrialPlanExecuting, s.Clock.Now().Format("2006-01-02T15:04:05.999999999Z07:00")); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "trial.start", "application", application.ID, policyRef, application, updated)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return updated, err
}
