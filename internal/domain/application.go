package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type ApplicationStatus string

const (
	ApplicationSubmitted    ApplicationStatus = "submitted"
	ApplicationQualified    ApplicationStatus = "qualified"
	ApplicationPlanApproved ApplicationStatus = "plan_approved"
	ApplicationAllocated    ApplicationStatus = "allocated"
	ApplicationRunning      ApplicationStatus = "running"
	ApplicationInterrupted  ApplicationStatus = "interrupted"
	ApplicationDataLocked   ApplicationStatus = "data_locked"
	ApplicationUnderReview  ApplicationStatus = "under_review"
	ApplicationPublished    ApplicationStatus = "published"
	ApplicationAdopted      ApplicationStatus = "adopted"
	ApplicationRejected     ApplicationStatus = "rejected"
	ApplicationCancelled    ApplicationStatus = "cancelled"
	ApplicationRevoked      ApplicationStatus = "revoked"
)

var applicationTransitions = map[ApplicationStatus]map[ApplicationStatus]bool{
	ApplicationSubmitted:    {ApplicationQualified: true, ApplicationRejected: true, ApplicationCancelled: true},
	ApplicationQualified:    {ApplicationPlanApproved: true, ApplicationRejected: true, ApplicationCancelled: true},
	ApplicationPlanApproved: {ApplicationAllocated: true, ApplicationCancelled: true},
	ApplicationAllocated:    {ApplicationRunning: true, ApplicationCancelled: true},
	ApplicationRunning:      {ApplicationInterrupted: true, ApplicationDataLocked: true},
	ApplicationInterrupted:  {ApplicationRunning: true, ApplicationCancelled: true, ApplicationDataLocked: true},
	ApplicationDataLocked:   {ApplicationUnderReview: true},
	ApplicationUnderReview:  {ApplicationPublished: true, ApplicationRevoked: true},
	ApplicationPublished:    {ApplicationAdopted: true, ApplicationRevoked: true},
	ApplicationAdopted:      {ApplicationAdopted: true, ApplicationRevoked: true},
	ApplicationRejected:     {},
	ApplicationCancelled:    {},
	ApplicationRevoked:      {},
}

func (s ApplicationStatus) Terminal() bool {
	return s == ApplicationRejected || s == ApplicationCancelled || s == ApplicationRevoked
}

func (s ApplicationStatus) CanTransition(next ApplicationStatus) bool {
	transitions, known := applicationTransitions[s]
	if !known {
		return false
	}
	allowed, present := transitions[next]
	if !present {
		return false
	}
	return allowed
}

type Variety struct {
	ID                 string    `json:"id"`
	OwnerInstitutionID string    `json:"owner_institution_id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	Crop               string    `json:"crop"`
	Generation         int       `json:"generation"`
	TraitsJSON         string    `json:"traits_json"`
	CreatedAt          time.Time `json:"created_at"`
}

func (v Variety) Validate() error {
	if strings.TrimSpace(v.Code) == "" || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Crop) == "" {
		return fmt.Errorf("variety identity fields: %w", apperror.ErrValidation)
	}
	if v.Generation < 1 {
		return fmt.Errorf("variety generation: %w", apperror.ErrValidation)
	}
	return nil
}

type Application struct {
	ID                     string            `json:"id"`
	VarietyID              string            `json:"variety_id"`
	ApplicantUserID        string            `json:"applicant_user_id"`
	ApplicantInstitutionID string            `json:"applicant_institution_id"`
	Region                 string            `json:"region"`
	Status                 ApplicationStatus `json:"status"`
	PolicyRef              string            `json:"policy_ref"`
	SubmissionNote         string            `json:"submission_note"`
	QualificationNote      string            `json:"qualification_note,omitempty"`
	Version                int64             `json:"version"`
	SubmittedAt            time.Time         `json:"submitted_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
}

func (a Application) Transition(next ApplicationStatus, now time.Time) (Application, error) {
	if !a.Status.CanTransition(next) {
		return Application{}, fmt.Errorf("application %s cannot move from %s to %s: %w", a.ID, a.Status, next, apperror.ErrInvalidState)
	}
	a.Status = next
	a.Version++
	a.UpdatedAt = now.UTC()
	return a, nil
}

func (a Application) ValidateForSubmission() error {
	if a.VarietyID == "" || a.ApplicantUserID == "" || a.ApplicantInstitutionID == "" {
		return fmt.Errorf("application references: %w", apperror.ErrValidation)
	}
	if strings.TrimSpace(a.Region) == "" || strings.TrimSpace(a.PolicyRef) == "" {
		return fmt.Errorf("application region and policy: %w", apperror.ErrValidation)
	}
	if len(strings.TrimSpace(a.SubmissionNote)) < 10 {
		return fmt.Errorf("application note must explain trial purpose: %w", apperror.ErrValidation)
	}
	if a.Status != ApplicationSubmitted || a.Version != 1 {
		return fmt.Errorf("new application state: %w", apperror.ErrValidation)
	}
	return nil
}

type TrialPlanStatus string

const (
	TrialPlanDraft     TrialPlanStatus = "draft"
	TrialPlanApproved  TrialPlanStatus = "approved"
	TrialPlanExecuting TrialPlanStatus = "executing"
	TrialPlanLocked    TrialPlanStatus = "locked"
	TrialPlanCancelled TrialPlanStatus = "cancelled"
)

type TrialPlan struct {
	ID                  string          `json:"id"`
	ApplicationID       string          `json:"application_id"`
	Season              string          `json:"season"`
	Region              string          `json:"region"`
	ObservationOpensAt  time.Time       `json:"observation_opens_at"`
	ObservationClosesAt time.Time       `json:"observation_closes_at"`
	RequiredReviewers   int             `json:"required_reviewers"`
	MaxReviewers        int             `json:"max_reviewers"`
	Status              TrialPlanStatus `json:"status"`
	Version             int64           `json:"version"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

func (p TrialPlan) Validate() error {
	if p.ApplicationID == "" || p.Season == "" || p.Region == "" {
		return fmt.Errorf("trial plan identity: %w", apperror.ErrValidation)
	}
	if !p.ObservationOpensAt.Before(p.ObservationClosesAt) {
		return fmt.Errorf("trial observation window: %w", apperror.ErrValidation)
	}
	if p.RequiredReviewers < 2 || p.MaxReviewers < p.RequiredReviewers {
		return fmt.Errorf("trial review quorum: %w", apperror.ErrValidation)
	}
	return nil
}

func (p TrialPlan) ObservationWindow(now time.Time) error {
	if now.Before(p.ObservationOpensAt) {
		return fmt.Errorf("observation window not open: %w", apperror.ErrInvalidState)
	}
	if !now.Before(p.ObservationClosesAt) {
		return fmt.Errorf("observation window closed: %w", apperror.ErrExpired)
	}
	return nil
}
