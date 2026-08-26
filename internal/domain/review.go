package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type ReviewDecision string

const (
	ReviewRecommend ReviewDecision = "recommend"
	ReviewReject    ReviewDecision = "reject"
	ReviewRevise    ReviewDecision = "revise"
)

type ExpertReview struct {
	ID            string         `json:"id"`
	ApplicationID string         `json:"application_id"`
	ExpertUserID  string         `json:"expert_user_id"`
	InstitutionID string         `json:"institution_id"`
	Decision      ReviewDecision `json:"decision"`
	Rationale     string         `json:"rationale"`
	PolicyRef     string         `json:"policy_ref"`
	SubmittedAt   time.Time      `json:"submitted_at"`
}

func (r ExpertReview) Validate() error {
	if r.ApplicationID == "" || r.ExpertUserID == "" || r.InstitutionID == "" {
		return fmt.Errorf("review references: %w", apperror.ErrValidation)
	}
	if r.Decision != ReviewRecommend && r.Decision != ReviewReject && r.Decision != ReviewRevise {
		return fmt.Errorf("review decision: %w", apperror.ErrValidation)
	}
	if len(strings.TrimSpace(r.Rationale)) < 20 || strings.TrimSpace(r.PolicyRef) == "" {
		return fmt.Errorf("review evidence: %w", apperror.ErrValidation)
	}
	return nil
}

type ConclusionStatus string

const (
	ConclusionDraft      ConclusionStatus = "draft"
	ConclusionPublished  ConclusionStatus = "published"
	ConclusionSuperseded ConclusionStatus = "superseded"
	ConclusionRevoked    ConclusionStatus = "revoked"
)

type Conclusion struct {
	ID            string           `json:"id"`
	ApplicationID string           `json:"application_id"`
	Version       int64            `json:"version"`
	Status        ConclusionStatus `json:"status"`
	Decision      ReviewDecision   `json:"decision"`
	Summary       string           `json:"summary"`
	PolicyRef     string           `json:"policy_ref"`
	PublishedBy   string           `json:"published_by,omitempty"`
	PublishedAt   *time.Time       `json:"published_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
}

func (c Conclusion) ValidateDraft() error {
	if c.ApplicationID == "" || c.Version < 1 {
		return fmt.Errorf("conclusion identity: %w", apperror.ErrValidation)
	}
	if c.Status != ConclusionDraft {
		return fmt.Errorf("conclusion status: %w", apperror.ErrInvalidState)
	}
	if len(strings.TrimSpace(c.Summary)) < 30 || strings.TrimSpace(c.PolicyRef) == "" {
		return fmt.Errorf("conclusion evidence: %w", apperror.ErrValidation)
	}
	return nil
}

type AdoptionStatus string

const (
	AdoptionActive  AdoptionStatus = "active"
	AdoptionRevoked AdoptionStatus = "revoked"
)

type RegionalAdoption struct {
	ID            string         `json:"id"`
	ConclusionID  string         `json:"conclusion_id"`
	Region        string         `json:"region"`
	InstitutionID string         `json:"institution_id"`
	Status        AdoptionStatus `json:"status"`
	PolicyRef     string         `json:"policy_ref"`
	AdoptedBy     string         `json:"adopted_by"`
	AdoptedAt     time.Time      `json:"adopted_at"`
	RevokedBy     string         `json:"revoked_by,omitempty"`
	RevokedAt     *time.Time     `json:"revoked_at,omitempty"`
	RevokeReason  string         `json:"revoke_reason,omitempty"`
}

func (a RegionalAdoption) Revoke(actor, reason string, now time.Time) (RegionalAdoption, error) {
	if a.Status != AdoptionActive {
		return RegionalAdoption{}, fmt.Errorf("adoption %s is %s: %w", a.ID, a.Status, apperror.ErrInvalidState)
	}
	if actor == "" || len(strings.TrimSpace(reason)) < 10 {
		return RegionalAdoption{}, fmt.Errorf("adoption revocation evidence: %w", apperror.ErrValidation)
	}
	a.Status = AdoptionRevoked
	a.RevokedBy = actor
	revoked := now.UTC()
	a.RevokedAt = &revoked
	a.RevokeReason = strings.TrimSpace(reason)
	return a, nil
}
