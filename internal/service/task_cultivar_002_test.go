package service

import (
 "context"
 "testing"
 "time"
 "github.com/11DingKing/cultivar-trial-governance/internal/audit"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func TestNormalTrialRejectsLateObservation(t *testing.T) {
 f := newServiceFixture(t); result := f.submit(t, "late-observation"); f.qualifyAndPlan(t, result.Application.ID); seed, plot := f.seedResources(t, result.Variety.ID, "late-observation")
 if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P"}); err != nil { t.Fatal(err) }
 if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "P"); err != nil { t.Fatal(err) }
 f.clock.Advance(2*time.Hour); batch, err := f.service.CreateObservationBatch(context.Background(), f.staff, CreateBatchInput{ApplicationID: result.Application.ID, WindowName: "late-window", OpensAt: f.clock.Now().Add(-time.Hour), ClosesAt: f.clock.Now().Add(-time.Minute), PolicyRef: "P"}); if err != nil { t.Fatal(err) }
 _, err = f.service.RecordObservation(audit.WithRequestID(context.Background(), "late"), f.staff, RecordObservationInput{BatchID: batch.ID, Metric: "height", Value: 10, Unit: "cm", ObservedAt: f.clock.Now(), Rule: domain.ObservationRule{Metric:"height", Unit:"cm", Min:0, Max:20}, PolicyRef:"P"})
 if err == nil { t.Fatalf("late normal observation was accepted: %v", err) }
 if _, err := f.db.ListObservations(context.Background(), batch.ID, true); err != nil { t.Fatal(err) }
}
