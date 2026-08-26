package domain

import (
	"fmt"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type JobType string

const (
	JobObservationReminder JobType = "observation_reminder"
	JobAnomalyReview       JobType = "anomaly_review"
	JobSeasonSummary       JobType = "season_summary"
	JobAdoptionFollowUp    JobType = "adoption_follow_up"
)

func (t JobType) Valid() bool {
	switch t {
	case JobObservationReminder, JobAnomalyReview, JobSeasonSummary, JobAdoptionFollowUp:
		return true
	default:
		return false
	}
}

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobDead      JobStatus = "dead"
)

type WorkerJob struct {
	ID             string     `json:"id"`
	Type           JobType    `json:"type"`
	ObjectType     string     `json:"object_type"`
	ObjectID       string     `json:"object_id"`
	PayloadJSON    string     `json:"payload_json"`
	Status         JobStatus  `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	AvailableAt    time.Time  `json:"available_at"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (j WorkerJob) Validate() error {
	if j.ID == "" || !j.Type.Valid() || j.ObjectType == "" || j.ObjectID == "" {
		return fmt.Errorf("worker job identity: %w", apperror.ErrValidation)
	}
	if j.MaxAttempts < 1 || j.Attempts < 0 || j.Attempts > j.MaxAttempts {
		return fmt.Errorf("worker attempts: %w", apperror.ErrValidation)
	}
	return nil
}

func (j WorkerJob) RetryAt(now time.Time) time.Time {
	shift := j.Attempts
	if shift < 1 {
		shift = 1
	}
	if shift > 8 {
		shift = 8
	}
	return now.Add(time.Duration(1<<uint(shift-1)) * time.Second)
}

type AuditEvent struct {
	ID            string    `json:"id"`
	ActorUserID   string    `json:"actor_user_id"`
	InstitutionID string    `json:"institution_id"`
	RequestID     string    `json:"request_id"`
	Action        string    `json:"action"`
	ObjectType    string    `json:"object_type"`
	ObjectID      string    `json:"object_id"`
	Outcome       string    `json:"outcome"`
	PolicyRef     string    `json:"policy_ref,omitempty"`
	BeforeJSON    string    `json:"before_json,omitempty"`
	AfterJSON     string    `json:"after_json,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (a AuditEvent) Validate() error {
	if a.ID == "" || a.Action == "" || a.ObjectType == "" || a.ObjectID == "" || a.Outcome == "" {
		return fmt.Errorf("audit identity: %w", apperror.ErrValidation)
	}
	if a.RequestID == "" {
		return fmt.Errorf("audit request correlation: %w", apperror.ErrValidation)
	}
	return nil
}
