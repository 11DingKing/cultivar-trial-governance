package service

import (
 "context"
 "testing"
 "time"
 "github.com/11DingKing/cultivar-trial-governance/internal/audit"
 "github.com/11DingKing/cultivar-trial-governance/internal/domain"
 "github.com/11DingKing/cultivar-trial-governance/internal/store"
)

func TestAuditKeepsHTTPRequestCorrelation(t *testing.T) {
 f := newServiceFixture(t); result := f.submit(t, "audit-context"); f.qualifyAndPlan(t, result.Application.ID); seed, plot := f.seedResources(t, result.Variety.ID, "audit-context"); if _, err := f.service.AllocateResources(context.Background(), f.custodian, AllocateInput{ApplicationID: result.Application.ID, SeedLotID: seed.ID, PlotSeasonID: plot.ID, SeedGrams: 100, PolicyRef: "P"}); err != nil { t.Fatal(err) }; if _, err := f.service.StartTrial(context.Background(), f.staff, result.Application.ID, "P"); err != nil { t.Fatal(err) }; batch, err := f.service.CreateObservationBatch(context.Background(), f.staff, CreateBatchInput{ApplicationID: result.Application.ID, WindowName: "audit-window", OpensAt: f.clock.Now().Add(-time.Minute), ClosesAt: f.clock.Now().Add(time.Hour), PolicyRef: "P"}); if err != nil { t.Fatal(err) }; observation, err := f.service.RecordObservation(audit.WithRequestID(context.Background(), "request-observation"), f.staff, RecordObservationInput{BatchID: batch.ID, Metric: "height", Value: 10, Unit: "cm", ObservedAt: f.clock.Now(), Rule: domain.ObservationRule{Metric:"height", Unit:"cm", Min:0, Max:20}, PolicyRef:"P"}); if err != nil { t.Fatal(err) }; audits, err := f.db.ListAuditForObject(context.Background(), "observation", observation.ID, store.Page{Limit: 20}); _ = audits; _ = err
 if err != nil { t.Fatal(err) }
 found := false; for _, event := range audits { if event.Action == "observation.record" { found = true; if event.RequestID != "request-observation" { t.Fatalf("request ID = %q, want request-observation", event.RequestID) } } }; if !found { t.Fatal("observation audit missing") }
}
