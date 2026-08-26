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

type SubmitReviewInput struct {
	ApplicationID string                `json:"-"`
	Decision      domain.ReviewDecision `json:"decision"`
	Rationale     string                `json:"rationale"`
	PolicyRef     string                `json:"policy_ref"`
}

func (s Service) SubmitReview(ctx context.Context, actor domain.Principal, input SubmitReviewInput) (domain.ExpertReview, error) {
	if err := actor.Require(domain.PermissionReviewTrial); err != nil {
		return domain.ExpertReview{}, err
	}
	id, err := s.IDs.New("review")
	if err != nil {
		return domain.ExpertReview{}, err
	}
	now := s.Clock.Now()
	review := domain.ExpertReview{
		ID: id, ApplicationID: input.ApplicationID, ExpertUserID: actor.UserID,
		InstitutionID: actor.InstitutionID, Decision: input.Decision,
		Rationale: strings.TrimSpace(input.Rationale), PolicyRef: strings.TrimSpace(input.PolicyRef), SubmittedAt: now,
	}
	if err := review.Validate(); err != nil {
		return domain.ExpertReview{}, err
	}
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
		if application.Status != domain.ApplicationDataLocked && application.Status != domain.ApplicationUnderReview {
			return fmt.Errorf("application %s is %s: %w", application.ID, application.Status, apperror.ErrInvalidState)
		}
		plan, err := s.Store.GetTrialPlanByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		if err := s.Store.InsertExpertReview(ctx, tx, review, plan.MaxReviewers); err != nil {
			return err
		}
		if application.Status == domain.ApplicationDataLocked {
			updated, err := application.Transition(domain.ApplicationUnderReview, now)
			if err != nil {
				return err
			}
			if err := s.Store.UpdateApplication(ctx, tx, updated, application.Version); err != nil {
				return err
			}
		}
		event, err := s.auditEvent(ctx, actor, "review.submit", "expert_review", review.ID, review.PolicyRef, nil, review)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return review, err
}

type DraftConclusionInput struct {
	ApplicationID string                `json:"-"`
	Decision      domain.ReviewDecision `json:"decision"`
	Summary       string                `json:"summary"`
	PolicyRef     string                `json:"policy_ref"`
}

func (s Service) DraftConclusion(ctx context.Context, actor domain.Principal, input DraftConclusionInput) (domain.Conclusion, error) {
	if err := actor.Require(domain.PermissionPublishConclusion); err != nil {
		return domain.Conclusion{}, err
	}
	id, err := s.IDs.New("conclusion")
	if err != nil {
		return domain.Conclusion{}, err
	}
	var conclusion domain.Conclusion
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		application, err := s.Store.GetApplication(ctx, tx, input.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		if application.Status != domain.ApplicationUnderReview {
			return fmt.Errorf("application %s is not under review: %w", application.ID, apperror.ErrInvalidState)
		}
		plan, err := s.Store.GetTrialPlanByApplication(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		total, recommends, err := s.Store.CountReviews(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		if total < plan.RequiredReviewers {
			return fmt.Errorf("review quorum %d/%d: %w", total, plan.RequiredReviewers, apperror.ErrCapacity)
		}
		if input.Decision == domain.ReviewRecommend && recommends < plan.RequiredReviewers {
			return fmt.Errorf("recommendation quorum %d/%d: %w", recommends, plan.RequiredReviewers, apperror.ErrConflict)
		}
		version, err := s.Store.NextConclusionVersion(ctx, tx, application.ID)
		if err != nil {
			return err
		}
		conclusion = domain.Conclusion{
			ID: id, ApplicationID: application.ID, Version: version, Status: domain.ConclusionDraft,
			Decision: input.Decision, Summary: strings.TrimSpace(input.Summary),
			PolicyRef: strings.TrimSpace(input.PolicyRef), CreatedAt: s.Clock.Now(),
		}
		if err := conclusion.ValidateDraft(); err != nil {
			return err
		}
		if err := s.Store.InsertConclusion(ctx, tx, conclusion); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "conclusion.draft", "conclusion", conclusion.ID, conclusion.PolicyRef, nil, conclusion)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return conclusion, err
}

func (s Service) PublishConclusion(ctx context.Context, actor domain.Principal, conclusionID string) (domain.Conclusion, error) {
	if err := actor.Require(domain.PermissionPublishConclusion); err != nil {
		return domain.Conclusion{}, err
	}
	var published domain.Conclusion
	err := s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		conclusion, err := s.Store.GetConclusion(ctx, tx, conclusionID)
		if err != nil {
			return err
		}
		application, err := s.Store.GetApplication(ctx, tx, conclusion.ApplicationID)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(application.Region); err != nil {
			return err
		}
		updated, err := application.Transition(domain.ApplicationPublished, s.Clock.Now())
		if err != nil {
			return err
		}
		if err := s.Store.PublishConclusion(ctx, tx, conclusion.ID, actor.UserID, s.Clock.Now().Format("2006-01-02T15:04:05.999999999Z07:00")); err != nil {
			return err
		}
		if err := s.Store.UpdateApplication(ctx, tx, updated, application.Version); err != nil {
			return err
		}
		published = conclusion
		published.Status = domain.ConclusionPublished
		published.PublishedBy = actor.UserID
		now := s.Clock.Now()
		published.PublishedAt = &now
		event, err := s.auditEvent(ctx, actor, "conclusion.publish", "conclusion", conclusion.ID, conclusion.PolicyRef, conclusion, published)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return published, err
}

type AdoptInput struct {
	ConclusionID  string `json:"conclusion_id"`
	InstitutionID string `json:"institution_id"`
	Region        string `json:"region"`
	PolicyRef     string `json:"policy_ref"`
}

func (s Service) AdoptConclusion(ctx context.Context, actor domain.Principal, input AdoptInput) (domain.RegionalAdoption, error) {
	if err := actor.Require(domain.PermissionAdoptRegion); err != nil {
		return domain.RegionalAdoption{}, err
	}
	if err := actor.RequireRegion(input.Region); err != nil {
		return domain.RegionalAdoption{}, err
	}
	id, err := s.IDs.New("adoption")
	if err != nil {
		return domain.RegionalAdoption{}, err
	}
	jobID, err := s.IDs.New("job")
	if err != nil {
		return domain.RegionalAdoption{}, err
	}
	now := s.Clock.Now()
	adoption := domain.RegionalAdoption{
		ID: id, ConclusionID: input.ConclusionID, Region: input.Region, InstitutionID: input.InstitutionID,
		Status: domain.AdoptionActive, PolicyRef: strings.TrimSpace(input.PolicyRef), AdoptedBy: actor.UserID, AdoptedAt: now,
	}
	err = s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		conclusion, err := s.Store.GetConclusion(ctx, tx, input.ConclusionID)
		if err != nil {
			return err
		}
		if conclusion.Status != domain.ConclusionPublished || conclusion.Decision != domain.ReviewRecommend {
			return fmt.Errorf("conclusion is not adoptable: %w", apperror.ErrInvalidState)
		}
		application, err := s.Store.GetApplication(ctx, tx, conclusion.ApplicationID)
		if err != nil {
			return err
		}
		if application.Region != input.Region {
			return fmt.Errorf("conclusion region differs from adoption region: %w", apperror.ErrConflict)
		}
		if application.Status == domain.ApplicationAdopted {
			return fmt.Errorf("application %s already adopted: %w", application.ID, apperror.ErrInvalidState)
		}
		if err := s.Store.InsertAdoption(ctx, tx, adoption); err != nil {
			return err
		}
		job := domain.WorkerJob{
			ID: jobID, Type: domain.JobAdoptionFollowUp, ObjectType: "regional_adoption", ObjectID: adoption.ID,
			PayloadJSON: `{"phase":"initial"}`, Status: domain.JobPending, MaxAttempts: s.WorkerMaxAttempts,
			AvailableAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now,
		}
		if err := s.Store.InsertJob(ctx, tx, job); err != nil {
			return err
		}
		updated, err := application.Transition(domain.ApplicationAdopted, now)
		if err != nil {
			return err
		}
		if err := s.Store.UpdateApplication(ctx, tx, updated, application.Version); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "adoption.create", "regional_adoption", adoption.ID, adoption.PolicyRef, nil, adoption)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return adoption, err
}

func (s Service) RevokeAdoption(ctx context.Context, actor domain.Principal, id, reason, policyRef string) (domain.RegionalAdoption, error) {
	if err := actor.Require(domain.PermissionRevokeAdoption); err != nil {
		return domain.RegionalAdoption{}, err
	}
	var revoked domain.RegionalAdoption
	err := s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		current, err := s.Store.GetAdoption(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := actor.RequireRegion(current.Region); err != nil {
			return err
		}
		revoked, err = current.Revoke(actor.UserID, reason, s.Clock.Now())
		if err != nil {
			return err
		}
		if err := s.Store.RevokeAdoption(ctx, tx, revoked); err != nil {
			return err
		}
		event, err := s.auditEvent(ctx, actor, "adoption.revoke", "regional_adoption", id, policyRef, current, revoked)
		if err != nil {
			return err
		}
		return s.Store.InsertAudit(ctx, tx, event)
	})
	return revoked, err
}
