package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type SubmitApplicationInput struct {
	VarietyCode    string `json:"variety_code"`
	VarietyName    string `json:"variety_name"`
	Crop           string `json:"crop"`
	Generation     int    `json:"generation"`
	TraitsJSON     string `json:"traits_json"`
	Region         string `json:"region"`
	PolicyRef      string `json:"policy_ref"`
	SubmissionNote string `json:"submission_note"`
	IdempotencyKey string `json:"-"`
}

type SubmitApplicationResult struct {
	Variety     domain.Variety     `json:"variety"`
	Application domain.Application `json:"application"`
}

func (s Service) SubmitApplication(ctx context.Context, actor domain.Principal, input SubmitApplicationInput) (SubmitApplicationResult, error) {
	if err := s.validate(); err != nil {
		return SubmitApplicationResult{}, err
	}
	if err := actor.Require(domain.PermissionSubmitApplication); err != nil {
		return SubmitApplicationResult{}, err
	}
	if err := actor.RequireRegion(input.Region); err != nil {
		return SubmitApplicationResult{}, err
	}
	if err := ensureIdempotencyKey(input.IdempotencyKey); err != nil {
		return SubmitApplicationResult{}, err
	}
	digest, err := requestDigest(input)
	if err != nil {
		return SubmitApplicationResult{}, err
	}
	if existing, ok, err := s.Store.GetIdempotency(ctx, nil, actor.InstitutionID, "POST", "/v1/applications", input.IdempotencyKey); err != nil {
		return SubmitApplicationResult{}, err
	} else if ok {
		if err := sameHash(existing, digest); err != nil {
			return SubmitApplicationResult{}, err
		}
		return decodeStored[SubmitApplicationResult](existing)
	}
	varietyID, err := s.IDs.New("variety")
	if err != nil {
		return SubmitApplicationResult{}, err
	}
	applicationID, err := s.IDs.New("application")
	if err != nil {
		return SubmitApplicationResult{}, err
	}
	now := s.Clock.Now()
	variety := domain.Variety{
		ID: varietyID, OwnerInstitutionID: actor.InstitutionID, Code: strings.TrimSpace(input.VarietyCode),
		Name: strings.TrimSpace(input.VarietyName), Crop: strings.TrimSpace(input.Crop), Generation: input.Generation,
		TraitsJSON: input.TraitsJSON, CreatedAt: now,
	}
	if variety.TraitsJSON == "" {
		variety.TraitsJSON = "{}"
	}
	application := domain.Application{
		ID: applicationID, VarietyID: variety.ID, ApplicantUserID: actor.UserID,
		ApplicantInstitutionID: actor.InstitutionID, Region: strings.TrimSpace(input.Region),
		Status: domain.ApplicationSubmitted, PolicyRef: strings.TrimSpace(input.PolicyRef),
		SubmissionNote: strings.TrimSpace(input.SubmissionNote), Version: 1, SubmittedAt: now, UpdatedAt: now,
	}
	if err := variety.Validate(); err != nil {
		return SubmitApplicationResult{}, err
	}
	if err := application.ValidateForSubmission(); err != nil {
		return SubmitApplicationResult{}, err
	}
	result := SubmitApplicationResult{Variety: variety, Application: application}
	response, err := encodeStored(result)
	if err != nil {
		return SubmitApplicationResult{}, err
	}
	auditEvent, err := s.auditEvent(ctx, actor, "application.submit", "application", application.ID, application.PolicyRef, nil, application)
	if err != nil {
		return SubmitApplicationResult{}, err
	}
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := checkContext(ctx, "submit application"); err != nil {
			return err
		}
		if err := s.Store.InsertVariety(ctx, tx, variety); err != nil {
			return err
		}
		if err := s.Store.InsertApplication(ctx, tx, application); err != nil {
			return err
		}
		if err := s.Store.InsertAudit(ctx, tx, auditEvent); err != nil {
			return err
		}
		return s.Store.InsertIdempotency(ctx, tx, store.IdempotencyRecord{
			InstitutionID: actor.InstitutionID, Method: "POST", Path: "/v1/applications",
			Key: input.IdempotencyKey, RequestHash: digest, ResponseCode: 201, ResponseJSON: response,
			CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: expiry(now),
		})
	})
	if err != nil {
		return SubmitApplicationResult{}, fmt.Errorf("submit application transaction: %w", err)
	}
	return result, nil
}

type QualificationInput struct {
	ApplicationID string `json:"-"`
	Approved      bool   `json:"approved"`
	Note          string `json:"note"`
	PolicyRef     string `json:"policy_ref"`
}

func (s Service) QualifyApplication(ctx context.Context, actor domain.Principal, input QualificationInput) (domain.Application, error) {
	if err := actor.Require(domain.PermissionVerifyQualification); err != nil {
		return domain.Application{}, err
	}
	if len(strings.TrimSpace(input.Note)) < 10 || strings.TrimSpace(input.PolicyRef) == "" {
		return domain.Application{}, fmt.Errorf("qualification evidence: %w", apperror.ErrValidation)
	}
	var updated domain.Application
	err := s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		current, err := s.Store.GetApplication(ctx, tx, input.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(current.Region); err != nil {
			return err
		}
		if err := domain.RequireSeparation(actor, current.ApplicantUserID, current.ApplicantInstitutionID); err != nil {
			return err
		}
		next := domain.ApplicationRejected
		if input.Approved {
			next = domain.ApplicationQualified
		}
		updated, err = current.Transition(next, s.Clock.Now())
		if err != nil {
			return err
		}
		updated.QualificationNote = strings.TrimSpace(input.Note)
		updated.PolicyRef = strings.TrimSpace(input.PolicyRef)
		if err := s.Store.UpdateApplication(ctx, tx, updated, current.Version); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "application.qualify", "application", current.ID, input.PolicyRef, current, updated)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return updated, err
}

type ApprovePlanInput struct {
	ApplicationID       string    `json:"-"`
	Season              string    `json:"season"`
	ObservationOpensAt  time.Time `json:"observation_opens_at"`
	ObservationClosesAt time.Time `json:"observation_closes_at"`
	RequiredReviewers   int       `json:"required_reviewers"`
	MaxReviewers        int       `json:"max_reviewers"`
	PolicyRef           string    `json:"policy_ref"`
}

func (s Service) ApprovePlan(ctx context.Context, actor domain.Principal, input ApprovePlanInput) (domain.TrialPlan, error) {
	if err := actor.Require(domain.PermissionApprovePlan); err != nil {
		return domain.TrialPlan{}, err
	}
	planID, err := s.IDs.New("plan")
	if err != nil {
		return domain.TrialPlan{}, err
	}
	now := s.Clock.Now()
	var plan domain.TrialPlan
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		application, err := s.Store.GetApplication(ctx, tx, input.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		if err := domain.RequireSeparation(actor, application.ApplicantUserID, application.ApplicantInstitutionID); err != nil {
			return err
		}
		updated, err := application.Transition(domain.ApplicationPlanApproved, now)
		if err != nil {
			return err
		}
		plan = domain.TrialPlan{
			ID: planID, ApplicationID: application.ID, Season: strings.TrimSpace(input.Season), Region: application.Region,
			ObservationOpensAt: input.ObservationOpensAt.UTC(), ObservationClosesAt: input.ObservationClosesAt.UTC(),
			RequiredReviewers: input.RequiredReviewers, MaxReviewers: input.MaxReviewers,
			Status: domain.TrialPlanApproved, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := plan.Validate(); err != nil {
			return err
		}
		if err := s.Store.InsertTrialPlan(ctx, tx, plan); err != nil {
			return err
		}
		if err := s.Store.UpdateApplication(ctx, tx, updated, application.Version); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "plan.approve", "trial_plan", plan.ID, input.PolicyRef, application, plan)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return plan, err
}
