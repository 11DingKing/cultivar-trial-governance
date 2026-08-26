package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

// interruptApplication moves a running application into the interrupted state
// directly through the store, mirroring the production recovery flow.
func (f *serviceFixture) interruptApplication(t *testing.T, applicationID string) domain.Application {
	t.Helper()
	ctx := context.Background()
	application, err := f.db.GetApplication(ctx, nil, applicationID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	interrupted, err := application.Transition(domain.ApplicationInterrupted, f.clock.Now())
	if err != nil {
		t.Fatalf("transition to interrupted: %v", err)
	}
	if err := f.db.UpdateApplication(ctx, nil, interrupted, application.Version); err != nil {
		t.Fatalf("update application: %v", err)
	}
	return interrupted
}

// prepareTrialForObservation submits an application, qualifies it, allocates
// resources, starts the trial and returns a closed observation batch owned by
// the now-running application.
func (f *serviceFixture) prepareTrialForObservation(t *testing.T, key string) (domain.Application, domain.ObservationBatch) {
	t.Helper()
	result := f.submit(t, key)
	f.qualifyAndPlan(t, result.Application.ID)
	seed, plot := f.seedResources(t, result.Variety.ID, key)
	if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{
		ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P",
	}); err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "P"); err != nil {
		t.Fatalf("start trial: %v", err)
	}
	f.clock.Advance(2 * time.Hour)
	closesAt := f.clock.Now().Add(-30 * time.Minute)
	batch, err := f.service.CreateObservationBatch(context.Background(), f.staff, CreateBatchInput{
		ApplicationID: result.Application.ID, WindowName: "seedling",
		OpensAt: f.clock.Now().Add(-time.Hour), ClosesAt: closesAt, PolicyRef: "P",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	application, err := f.db.GetApplication(context.Background(), nil, result.Application.ID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	return application, batch
}

// TestRecordObservationRejectsLateReportForOrdinaryTrial guards against the
// regression where the recovery grace window was always enabled: a trial that
// is merely running must not accept observations reported after the window
// closes, otherwise the late data would be treated as a valid sample at lock.
func TestRecordObservationRejectsLateReportForOrdinaryTrial(t *testing.T) {
	f := newServiceFixture(t)
	_, batch := f.prepareTrialForObservation(t, "late-ordinary")
	rule := domain.ObservationRule{Metric: "height", Unit: "cm", Min: 1, Max: 100}
	// A late report lands one hour after the window has already closed.
	_, err := f.service.RecordObservation(context.Background(), f.staff, RecordObservationInput{
		BatchID: batch.ID, Metric: "height", Value: 12, Unit: "cm",
		ObservedAt: batch.ClosesAt.Add(-time.Minute), Rule: rule, PolicyRef: "P",
	})
	if !errors.Is(err, apperror.ErrExpired) {
		t.Fatalf("ordinary trial late report error = %v, want expired", err)
	}
}

// TestRecordObservationAcceptsLateReportForInterruptedTrialDuringRecoveryGrace
// confirms that only an interrupted trial may receive late observations inside
// the recovery grace window.
func TestRecordObservationAcceptsLateReportForInterruptedTrialDuringRecoveryGrace(t *testing.T) {
	f := newServiceFixture(t)
	application, batch := f.prepareTrialForObservation(t, "late-interrupted")
	f.interruptApplication(t, application.ID)
	rule := domain.ObservationRule{Metric: "height", Unit: "cm", Min: 1, Max: 100}
	observation, err := f.service.RecordObservation(context.Background(), f.staff, RecordObservationInput{
		BatchID: batch.ID, Metric: "height", Value: 12, Unit: "cm",
		ObservedAt: batch.ClosesAt.Add(-time.Minute), Rule: rule, PolicyRef: "P",
	})
	if err != nil {
		t.Fatalf("interrupted trial should accept late report within grace: %v", err)
	}
	if observation.Invalidated {
		t.Fatal("late observation inside recovery grace must remain valid")
	}
}
