package domain

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

func TestObservationBatchAcceptsRecoveryGraceOnlyForInterruptedTrial(t *testing.T) {
	t.Parallel()
	opens := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	closes := opens.Add(12 * time.Hour)
	batch := ObservationBatch{ID: "batch", OpensAt: opens, ClosesAt: closes, Status: ObservationOpen}
	if !errors.Is(batch.Accepts(opens.Add(-time.Second), false), apperror.ErrInvalidState) {
		t.Fatal("pre-window observation must fail")
	}
	if err := batch.Accepts(opens, false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(batch.Accepts(closes, false), apperror.ErrExpired) {
		t.Fatal("ordinary trial should reject closing instant")
	}
	if err := batch.Accepts(closes.Add(23*time.Hour), true); err != nil {
		t.Fatalf("interrupted trial should receive grace: %v", err)
	}
	if !errors.Is(batch.Accepts(closes.Add(24*time.Hour), true), apperror.ErrExpired) {
		t.Fatal("grace interval must be half-open")
	}
}

func TestObservationBatchLockCreatesImmutableValue(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	batch := ObservationBatch{ID: "batch", Status: ObservationOpen, Version: 3, UpdatedAt: now.Add(-time.Hour)}
	locked, err := batch.Lock(now)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Status != ObservationLocked || locked.Version != 4 || locked.LockedAt == nil || !locked.LockedAt.Equal(now) {
		t.Fatalf("unexpected locked batch: %+v", locked)
	}
	if batch.Status != ObservationOpen || batch.LockedAt != nil {
		t.Fatalf("source batch mutated: %+v", batch)
	}
	if _, err := locked.Lock(now); !errors.Is(err, apperror.ErrInvalidState) {
		t.Fatalf("repeated lock error = %v", err)
	}
}

func TestObservationValidationRejectsNonFiniteAndIncompleteValues(t *testing.T) {
	t.Parallel()
	valid := Observation{BatchID: "b", Metric: "height", Value: 12.5, Unit: "cm", ObservedAt: time.Now(), ReportedBy: "u"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	values := []Observation{
		{Metric: valid.Metric, Unit: valid.Unit, ObservedAt: valid.ObservedAt, ReportedBy: valid.ReportedBy},
		{BatchID: valid.BatchID, Unit: valid.Unit, ObservedAt: valid.ObservedAt, ReportedBy: valid.ReportedBy},
		{BatchID: valid.BatchID, Metric: valid.Metric, ObservedAt: valid.ObservedAt, ReportedBy: valid.ReportedBy},
		{BatchID: valid.BatchID, Metric: valid.Metric, Unit: valid.Unit, ReportedBy: valid.ReportedBy},
		{BatchID: valid.BatchID, Metric: valid.Metric, Unit: valid.Unit, ObservedAt: valid.ObservedAt},
		{BatchID: valid.BatchID, Metric: valid.Metric, Value: math.NaN(), Unit: valid.Unit, ObservedAt: valid.ObservedAt, ReportedBy: valid.ReportedBy},
		{BatchID: valid.BatchID, Metric: valid.Metric, Value: math.Inf(1), Unit: valid.Unit, ObservedAt: valid.ObservedAt, ReportedBy: valid.ReportedBy},
	}
	for _, value := range values {
		if !errors.Is(value.Validate(), apperror.ErrValidation) {
			t.Fatalf("expected invalid observation: %+v", value)
		}
	}
}

func TestObservationRuleFlagsBothTails(t *testing.T) {
	t.Parallel()
	rule := ObservationRule{Metric: "germination", Unit: "percent", Min: 80, Max: 100}
	for _, tc := range []struct {
		value     float64
		anomalous bool
	}{{79.9, true}, {80, false}, {90, false}, {100, false}, {100.1, true}} {
		got, err := rule.Evaluate(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.anomalous {
			t.Fatalf("value %v anomaly = %v, want %v", tc.value, got, tc.anomalous)
		}
	}
	if _, err := (ObservationRule{Metric: "x", Unit: "u", Min: 5, Max: 4}).Evaluate(4); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("invalid rule error = %v", err)
	}
}

func TestReviewAndConclusionEvidenceValidation(t *testing.T) {
	t.Parallel()
	review := ExpertReview{
		ApplicationID: "a", ExpertUserID: "e", InstitutionID: "i", Decision: ReviewRecommend,
		Rationale: "跨区域对照数据显示性状稳定且达到政策要求", PolicyRef: "POLICY-9",
	}
	if err := review.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []ReviewDecision{"", "maybe", "approve"} {
		value := review
		value.Decision = decision
		if !errors.Is(value.Validate(), apperror.ErrValidation) {
			t.Fatalf("decision %q should be invalid", decision)
		}
	}
	conclusion := Conclusion{
		ApplicationID: "a", Version: 1, Status: ConclusionDraft, Decision: ReviewRecommend,
		Summary: "综合多站点观测及专家复核证据，建议在目标区域按政策约束采用", PolicyRef: "POLICY-9",
	}
	if err := conclusion.ValidateDraft(); err != nil {
		t.Fatal(err)
	}
	conclusion.Status = ConclusionPublished
	if !errors.Is(conclusion.ValidateDraft(), apperror.ErrInvalidState) {
		t.Fatal("published conclusion cannot be validated as draft")
	}
}

func TestAdoptionRevocationKeepsHistory(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	active := RegionalAdoption{ID: "adopt", Status: AdoptionActive, AdoptedBy: "planner", AdoptedAt: now.Add(-time.Hour)}
	revoked, err := active.Revoke("planner-2", "新版本政策要求停止区域采用", now)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Status != AdoptionRevoked || revoked.RevokedBy != "planner-2" || revoked.RevokedAt == nil {
		t.Fatalf("unexpected revocation: %+v", revoked)
	}
	if revoked.AdoptedBy != active.AdoptedBy || !revoked.AdoptedAt.Equal(active.AdoptedAt) {
		t.Fatal("revocation must retain adoption history")
	}
	if _, err := revoked.Revoke("planner", "再次撤销同一采用记录", now); !errors.Is(err, apperror.ErrInvalidState) {
		t.Fatalf("repeated revoke error = %v", err)
	}
	if _, err := active.Revoke("planner", "短", now); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("short reason error = %v", err)
	}
}

func TestWorkerJobBackoffAndValidation(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	job := WorkerJob{ID: "j", Type: JobAnomalyReview, ObjectType: "batch", ObjectID: "b", Attempts: 1, MaxAttempts: 5}
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	previous := time.Duration(0)
	for attempts := 1; attempts <= 10; attempts++ {
		job.Attempts = attempts
		delay := job.RetryAt(now).Sub(now)
		if delay < previous {
			t.Fatalf("backoff decreased at attempt %d: %s < %s", attempts, delay, previous)
		}
		previous = delay
	}
	invalid := job
	invalid.Type = "unknown"
	if !errors.Is(invalid.Validate(), apperror.ErrValidation) {
		t.Fatal("unknown job type should fail")
	}
	invalid = job
	invalid.Attempts = invalid.MaxAttempts + 1
	if !errors.Is(invalid.Validate(), apperror.ErrValidation) {
		t.Fatal("attempt overflow should fail")
	}
}
