package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertExpertReview(ctx context.Context, executor Executor, value domain.ExpertReview, maxReviewers int) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	var count int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM expert_reviews WHERE application_id=?`, value.ApplicationID).Scan(&count); err != nil {
		return fmt.Errorf("count expert reviews: %w", err)
	}
	if count >= maxReviewers {
		return fmt.Errorf("review quota %d reached: %w", maxReviewers, apperror.ErrCapacity)
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO expert_reviews(
        id,application_id,expert_user_id,institution_id,decision,rationale,policy_ref,submitted_at
    ) VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.ApplicationID, value.ExpertUserID, value.InstitutionID,
		value.Decision, value.Rationale, value.PolicyRef, formatTime(value.SubmittedAt))
	if err != nil {
		return fmt.Errorf("insert expert review %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) CountReviews(ctx context.Context, executor Executor, applicationID string) (int, int, error) {
	if executor == nil {
		executor = d.sql
	}
	var total, recommend int
	err := executor.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN decision='recommend' THEN 1 ELSE 0 END),0)
        FROM expert_reviews WHERE application_id=?`, applicationID).Scan(&total, &recommend)
	if err != nil {
		return 0, 0, fmt.Errorf("count reviews for %s: %w", applicationID, err)
	}
	return total, recommend, nil
}

func (d *DB) NextConclusionVersion(ctx context.Context, executor Executor, applicationID string) (int64, error) {
	if executor == nil {
		executor = d.sql
	}
	var version int64
	if err := executor.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM conclusions WHERE application_id=?`, applicationID).Scan(&version); err != nil {
		return 0, fmt.Errorf("next conclusion version: %w", err)
	}
	return version, nil
}

func (d *DB) InsertConclusion(ctx context.Context, executor Executor, value domain.Conclusion) error {
	if err := value.ValidateDraft(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO conclusions(
        id,application_id,version,status,decision,summary,policy_ref,published_by,published_at,created_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ApplicationID, value.Version, value.Status,
		value.Decision, value.Summary, value.PolicyRef, nil, nil, formatTime(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert conclusion %s: %w", value.ID, err)
	}
	return nil
}

func scanConclusion(scanner interface{ Scan(...any) error }) (domain.Conclusion, error) {
	var value domain.Conclusion
	var status, decision, created string
	var publishedBy, publishedAt sql.NullString
	err := scanner.Scan(&value.ID, &value.ApplicationID, &value.Version, &status, &decision, &value.Summary,
		&value.PolicyRef, &publishedBy, &publishedAt, &created)
	if err != nil {
		return domain.Conclusion{}, err
	}
	value.Status = domain.ConclusionStatus(status)
	value.Decision = domain.ReviewDecision(decision)
	value.PublishedBy = publishedBy.String
	if value.PublishedAt, err = optionalTime(publishedAt); err != nil {
		return domain.Conclusion{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.Conclusion{}, err
	}
	return value, nil
}

func (d *DB) GetConclusion(ctx context.Context, executor Executor, id string) (domain.Conclusion, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanConclusion(executor.QueryRowContext(ctx, `SELECT id,application_id,version,status,
        decision,summary,policy_ref,published_by,published_at,created_at FROM conclusions WHERE id=?`, id))
	if err != nil {
		return domain.Conclusion{}, translateNotFound("conclusion", id, err)
	}
	return value, nil
}

func (d *DB) GetPublishedConclusion(ctx context.Context, executor Executor, applicationID string) (domain.Conclusion, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanConclusion(executor.QueryRowContext(ctx, `SELECT id,application_id,version,status,
        decision,summary,policy_ref,published_by,published_at,created_at FROM conclusions
        WHERE application_id=? AND status='published'`, applicationID))
	if err != nil {
		return domain.Conclusion{}, translateNotFound("published_conclusion", applicationID, err)
	}
	return value, nil
}

func (d *DB) PublishConclusion(ctx context.Context, executor Executor, id, actor, publishedAt string) error {
	if executor == nil {
		executor = d.sql
	}
	var applicationID string
	if err := executor.QueryRowContext(ctx, `SELECT application_id FROM conclusions WHERE id=? AND status='draft'`, id).Scan(&applicationID); err != nil {
		return translateNotFound("draft_conclusion", id, err)
	}
	if _, err := executor.ExecContext(ctx, `UPDATE conclusions SET status='superseded'
        WHERE application_id=? AND status='published'`, applicationID); err != nil {
		return fmt.Errorf("supersede current conclusion: %w", err)
	}
	result, err := executor.ExecContext(ctx, `UPDATE conclusions SET status='published',published_by=?,published_at=?
        WHERE id=? AND status='draft'`, actor, publishedAt, id)
	if err != nil {
		return fmt.Errorf("publish conclusion %s: %w", id, err)
	}
	return expectOne(result, "conclusion", id)
}

func (d *DB) InsertAdoption(ctx context.Context, executor Executor, value domain.RegionalAdoption) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO regional_adoptions(
        id,conclusion_id,region,institution_id,status,policy_ref,adopted_by,adopted_at,
        revoked_by,revoked_at,revoke_reason
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ConclusionID, value.Region, value.InstitutionID,
		value.Status, value.PolicyRef, value.AdoptedBy, formatTime(value.AdoptedAt), nil, nil, "")
	if err != nil {
		return fmt.Errorf("insert regional adoption %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) GetAdoption(ctx context.Context, executor Executor, id string) (domain.RegionalAdoption, error) {
	if executor == nil {
		executor = d.sql
	}
	var value domain.RegionalAdoption
	var status, adopted string
	var revokedBy, revokedAt sql.NullString
	err := executor.QueryRowContext(ctx, `SELECT id,conclusion_id,region,institution_id,status,policy_ref,
        adopted_by,adopted_at,revoked_by,revoked_at,revoke_reason FROM regional_adoptions WHERE id=?`, id).Scan(
		&value.ID, &value.ConclusionID, &value.Region, &value.InstitutionID, &status, &value.PolicyRef,
		&value.AdoptedBy, &adopted, &revokedBy, &revokedAt, &value.RevokeReason)
	if err != nil {
		return domain.RegionalAdoption{}, translateNotFound("adoption", id, err)
	}
	value.Status = domain.AdoptionStatus(status)
	value.RevokedBy = revokedBy.String
	if value.AdoptedAt, err = parseTime(adopted); err != nil {
		return domain.RegionalAdoption{}, err
	}
	if value.RevokedAt, err = optionalTime(revokedAt); err != nil {
		return domain.RegionalAdoption{}, err
	}
	return value, nil
}

func (d *DB) RevokeAdoption(ctx context.Context, executor Executor, value domain.RegionalAdoption) error {
	if executor == nil {
		executor = d.sql
	}
	result, err := executor.ExecContext(ctx, `UPDATE regional_adoptions SET status=?,revoked_by=?,revoked_at=?,revoke_reason=?
        WHERE id=? AND status='active'`, value.Status, value.RevokedBy, formatTime(*value.RevokedAt), value.RevokeReason, value.ID)
	if err != nil {
		return fmt.Errorf("revoke adoption %s: %w", value.ID, err)
	}
	return expectOne(result, "adoption", value.ID)
}
