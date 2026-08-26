package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertVariety(ctx context.Context, executor Executor, value domain.Variety) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO varieties(
        id,owner_institution_id,code,name,crop,generation,traits_json,created_at
    ) VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.OwnerInstitutionID, value.Code, value.Name, value.Crop,
		value.Generation, value.TraitsJSON, formatTime(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert variety %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) GetVariety(ctx context.Context, id string) (domain.Variety, error) {
	var value domain.Variety
	var created string
	err := d.sql.QueryRowContext(ctx, `SELECT id,owner_institution_id,code,name,crop,generation,traits_json,created_at FROM varieties WHERE id=?`, id).
		Scan(&value.ID, &value.OwnerInstitutionID, &value.Code, &value.Name, &value.Crop, &value.Generation, &value.TraitsJSON, &created)
	if err != nil {
		return domain.Variety{}, translateNotFound("variety", id, err)
	}
	value.CreatedAt, err = parseTime(created)
	return value, err
}

func (d *DB) InsertApplication(ctx context.Context, executor Executor, value domain.Application) error {
	if err := value.ValidateForSubmission(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO applications(
        id,variety_id,applicant_user_id,applicant_institution_id,region,status,policy_ref,
        submission_note,qualification_note,version,submitted_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.VarietyID, value.ApplicantUserID,
		value.ApplicantInstitutionID, value.Region, value.Status, value.PolicyRef, value.SubmissionNote,
		value.QualificationNote, value.Version, formatTime(value.SubmittedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert application %s: %w", value.ID, err)
	}
	return nil
}

const applicationColumns = `id,variety_id,applicant_user_id,applicant_institution_id,region,status,policy_ref,submission_note,qualification_note,version,submitted_at,updated_at`

func scanApplication(scanner interface{ Scan(...any) error }) (domain.Application, error) {
	var value domain.Application
	var status, submitted, updated string
	err := scanner.Scan(&value.ID, &value.VarietyID, &value.ApplicantUserID, &value.ApplicantInstitutionID,
		&value.Region, &status, &value.PolicyRef, &value.SubmissionNote, &value.QualificationNote,
		&value.Version, &submitted, &updated)
	if err != nil {
		return domain.Application{}, err
	}
	value.Status = domain.ApplicationStatus(status)
	if value.SubmittedAt, err = parseTime(submitted); err != nil {
		return domain.Application{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.Application{}, err
	}
	return value, nil
}

func (d *DB) GetApplication(ctx context.Context, executor Executor, id string) (domain.Application, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanApplication(executor.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=?`, id))
	if err != nil {
		return domain.Application{}, translateNotFound("application", id, err)
	}
	return value, nil
}

type ApplicationFilter struct {
	Region        string
	InstitutionID string
	Status        domain.ApplicationStatus
	Page          Page
}

func (d *DB) ListApplications(ctx context.Context, filter ApplicationFilter) ([]domain.Application, int, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 6)
	if filter.Region != "" {
		conditions = append(conditions, "region=?")
		args = append(args, filter.Region)
	}
	if filter.InstitutionID != "" {
		conditions = append(conditions, "applicant_institution_id=?")
		args = append(args, filter.InstitutionID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, filter.Status)
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count applications: %w", err)
	}
	page := filter.Page.Normalize()
	rows, err := d.sql.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE `+where+` ORDER BY submitted_at DESC,id LIMIT ? OFFSET ?`, append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()
	values := make([]domain.Application, 0, page.Limit)
	for rows.Next() {
		value, err := scanApplication(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan application list: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate applications: %w", err)
	}
	return values, total, nil
}

func (d *DB) UpdateApplication(ctx context.Context, executor Executor, value domain.Application, expectedVersion int64) error {
	if executor == nil {
		executor = d.sql
	}
	result, err := executor.ExecContext(ctx, `UPDATE applications SET
        status=?,qualification_note=?,policy_ref=?,version=?,updated_at=?
        WHERE id=? AND version=?`, value.Status, value.QualificationNote, value.PolicyRef,
		value.Version, formatTime(value.UpdatedAt), value.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update application %s: %w", value.ID, err)
	}
	return expectOne(result, "application", value.ID)
}

func (d *DB) InsertTrialPlan(ctx context.Context, executor Executor, value domain.TrialPlan) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO trial_plans(
        id,application_id,season,region,observation_opens_at,observation_closes_at,
        required_reviewers,max_reviewers,status,version,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ApplicationID, value.Season, value.Region,
		formatTime(value.ObservationOpensAt), formatTime(value.ObservationClosesAt), value.RequiredReviewers,
		value.MaxReviewers, value.Status, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert trial plan %s: %w", value.ID, err)
	}
	return nil
}

func scanTrialPlan(scanner interface{ Scan(...any) error }) (domain.TrialPlan, error) {
	var value domain.TrialPlan
	var opens, closes, status, created, updated string
	err := scanner.Scan(&value.ID, &value.ApplicationID, &value.Season, &value.Region, &opens, &closes,
		&value.RequiredReviewers, &value.MaxReviewers, &status, &value.Version, &created, &updated)
	if err != nil {
		return domain.TrialPlan{}, err
	}
	value.Status = domain.TrialPlanStatus(status)
	if value.ObservationOpensAt, err = parseTime(opens); err != nil {
		return domain.TrialPlan{}, err
	}
	if value.ObservationClosesAt, err = parseTime(closes); err != nil {
		return domain.TrialPlan{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.TrialPlan{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.TrialPlan{}, err
	}
	return value, nil
}

func (d *DB) GetTrialPlanByApplication(ctx context.Context, executor Executor, applicationID string) (domain.TrialPlan, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanTrialPlan(executor.QueryRowContext(ctx, `SELECT id,application_id,season,region,
        observation_opens_at,observation_closes_at,required_reviewers,max_reviewers,status,version,created_at,updated_at
        FROM trial_plans WHERE application_id=?`, applicationID))
	if err != nil {
		return domain.TrialPlan{}, translateNotFound("trial_plan", applicationID, err)
	}
	return value, nil
}

func (d *DB) UpdateTrialPlanStatus(ctx context.Context, executor Executor, id string, from, to domain.TrialPlanStatus, now string) error {
	if executor == nil {
		executor = d.sql
	}
	result, err := executor.ExecContext(ctx, `UPDATE trial_plans SET status=?,version=version+1,updated_at=? WHERE id=? AND status=?`, to, now, id, from)
	if err != nil {
		return fmt.Errorf("update trial plan %s: %w", id, err)
	}
	return expectOne(result, "trial_plan", id)
}

func (d *DB) ApplicationExists(ctx context.Context, id string) (bool, error) {
	var marker int
	err := d.sql.QueryRowContext(ctx, `SELECT 1 FROM applications WHERE id=?`, id).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check application existence: %w", err)
}
