package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

func TestApplicationLifecycleAllowsCompleteGovernancePath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	application := Application{
		ID: "application_1", VarietyID: "variety_1", ApplicantUserID: "breeder_1",
		ApplicantInstitutionID: "institution_1", Region: "north", Status: ApplicationSubmitted,
		PolicyRef: "POLICY-2026-01", SubmissionNote: "申请跨站点开展候选品种区域试验",
		Version: 1, SubmittedAt: now, UpdatedAt: now,
	}
	path := []ApplicationStatus{
		ApplicationQualified,
		ApplicationPlanApproved,
		ApplicationAllocated,
		ApplicationRunning,
		ApplicationDataLocked,
		ApplicationUnderReview,
		ApplicationPublished,
		ApplicationAdopted,
		ApplicationRevoked,
	}
	for index, next := range path {
		updated, err := application.Transition(next, now.Add(time.Duration(index+1)*time.Hour))
		if err != nil {
			t.Fatalf("transition %s -> %s failed: %v", application.Status, next, err)
		}
		if updated.Status != next {
			t.Fatalf("status = %s, want %s", updated.Status, next)
		}
		if updated.Version != application.Version+1 {
			t.Fatalf("version = %d, want %d", updated.Version, application.Version+1)
		}
		if !updated.UpdatedAt.After(application.UpdatedAt) {
			t.Fatalf("updated timestamp did not advance: %s", updated.UpdatedAt)
		}
		application = updated
	}
	if !application.Status.Terminal() {
		t.Fatalf("revoked application must be terminal")
	}
}

func TestApplicationLifecycleSupportsInterruptionAndRecovery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)
	application := Application{ID: "a", Status: ApplicationRunning, Version: 5, UpdatedAt: now}
	interrupted, err := application.Transition(ApplicationInterrupted, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != ApplicationInterrupted {
		t.Fatalf("status = %s", interrupted.Status)
	}
	recovered, err := interrupted.Transition(ApplicationRunning, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Version != 7 {
		t.Fatalf("version = %d, want 7", recovered.Version)
	}
}

func TestApplicationRejectsIllegalTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		from ApplicationStatus
		to   ApplicationStatus
	}{
		{"submitted cannot publish", ApplicationSubmitted, ApplicationPublished},
		{"qualified cannot run", ApplicationQualified, ApplicationRunning},
		{"allocated cannot review", ApplicationAllocated, ApplicationUnderReview},
		{"locked cannot return to running", ApplicationDataLocked, ApplicationRunning},
		{"published cannot cancel", ApplicationPublished, ApplicationCancelled},
		{"rejected cannot qualify", ApplicationRejected, ApplicationQualified},
		{"cancelled cannot allocate", ApplicationCancelled, ApplicationAllocated},
		{"revoked cannot adopt", ApplicationRevoked, ApplicationAdopted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			application := Application{ID: "application", Status: tc.from, Version: 4}
			_, err := application.Transition(tc.to, time.Now())
			if !errors.Is(err, apperror.ErrInvalidState) {
				t.Fatalf("error = %v, want ErrInvalidState", err)
			}
			if application.Status != tc.from || application.Version != 4 {
				t.Fatalf("source value was mutated: %+v", application)
			}
		})
	}
}

func TestApplicationSubmissionValidation(t *testing.T) {
	t.Parallel()
	valid := Application{
		ID: "a", VarietyID: "v", ApplicantUserID: "u", ApplicantInstitutionID: "i",
		Region: "north", Status: ApplicationSubmitted, PolicyRef: "P-1",
		SubmissionNote: "提交完整的区域试验目标说明", Version: 1,
	}
	if err := valid.ValidateForSubmission(); err != nil {
		t.Fatalf("valid application: %v", err)
	}
	mutations := []struct {
		name string
		edit func(*Application)
	}{
		{"missing variety", func(a *Application) { a.VarietyID = "" }},
		{"missing applicant", func(a *Application) { a.ApplicantUserID = "" }},
		{"missing institution", func(a *Application) { a.ApplicantInstitutionID = "" }},
		{"missing region", func(a *Application) { a.Region = "" }},
		{"missing policy", func(a *Application) { a.PolicyRef = "" }},
		{"short note", func(a *Application) { a.SubmissionNote = "太短" }},
		{"wrong state", func(a *Application) { a.Status = ApplicationQualified }},
		{"wrong version", func(a *Application) { a.Version = 2 }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			value := valid
			tc.edit(&value)
			if !errors.Is(value.ValidateForSubmission(), apperror.ErrValidation) {
				t.Fatalf("expected validation error for %+v", value)
			}
		})
	}
}

func TestTrialPlanValidatesWindowAndQuorum(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	valid := TrialPlan{
		ApplicationID: "a", Season: "2026-summer", Region: "north",
		ObservationOpensAt: now, ObservationClosesAt: now.Add(30 * 24 * time.Hour),
		RequiredReviewers: 2, MaxReviewers: 4,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*TrialPlan){
		func(p *TrialPlan) { p.ApplicationID = "" },
		func(p *TrialPlan) { p.Season = "" },
		func(p *TrialPlan) { p.Region = "" },
		func(p *TrialPlan) { p.ObservationClosesAt = p.ObservationOpensAt },
		func(p *TrialPlan) { p.RequiredReviewers = 1 },
		func(p *TrialPlan) { p.MaxReviewers = 1 },
	} {
		value := valid
		edit(&value)
		if !errors.Is(value.Validate(), apperror.ErrValidation) {
			t.Fatalf("expected validation error for %+v", value)
		}
	}
}

func TestTrialPlanObservationWindowUsesHalfOpenInterval(t *testing.T) {
	t.Parallel()
	opens := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	closes := opens.Add(8 * time.Hour)
	plan := TrialPlan{ObservationOpensAt: opens, ObservationClosesAt: closes}
	if !errors.Is(plan.ObservationWindow(opens.Add(-time.Nanosecond)), apperror.ErrInvalidState) {
		t.Fatal("time before open should be rejected")
	}
	if err := plan.ObservationWindow(opens); err != nil {
		t.Fatalf("opening instant should be accepted: %v", err)
	}
	if err := plan.ObservationWindow(closes.Add(-time.Nanosecond)); err != nil {
		t.Fatalf("instant before close should be accepted: %v", err)
	}
	if !errors.Is(plan.ObservationWindow(closes), apperror.ErrExpired) {
		t.Fatal("closing instant should be rejected")
	}
}

func TestVarietyValidation(t *testing.T) {
	t.Parallel()
	valid := Variety{Code: "CV-001", Name: "北丰一号", Crop: "maize", Generation: 3}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, value := range []Variety{
		{Code: "", Name: valid.Name, Crop: valid.Crop, Generation: 1},
		{Code: valid.Code, Name: "", Crop: valid.Crop, Generation: 1},
		{Code: valid.Code, Name: valid.Name, Crop: "", Generation: 1},
		{Code: valid.Code, Name: valid.Name, Crop: valid.Crop, Generation: 0},
	} {
		if !errors.Is(value.Validate(), apperror.ErrValidation) {
			t.Fatalf("expected validation error for %+v", value)
		}
	}
}
