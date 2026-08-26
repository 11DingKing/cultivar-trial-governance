package domain

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type ObservationBatchStatus string

const (
	ObservationOpen      ObservationBatchStatus = "open"
	ObservationLocked    ObservationBatchStatus = "locked"
	ObservationCancelled ObservationBatchStatus = "cancelled"
)

type ObservationBatch struct {
	ID            string                 `json:"id"`
	ApplicationID string                 `json:"application_id"`
	WindowName    string                 `json:"window_name"`
	OpensAt       time.Time              `json:"opens_at"`
	ClosesAt      time.Time              `json:"closes_at"`
	Status        ObservationBatchStatus `json:"status"`
	Version       int64                  `json:"version"`
	LockedAt      *time.Time             `json:"locked_at,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

func (b ObservationBatch) Accepts(now time.Time, interrupted bool) error {
	if b.Status != ObservationOpen {
		return fmt.Errorf("batch %s is %s: %w", b.ID, b.Status, apperror.ErrInvalidState)
	}
	if now.Before(b.OpensAt) {
		return fmt.Errorf("batch %s not open: %w", b.ID, apperror.ErrInvalidState)
	}
	if now.Before(b.ClosesAt) {
		return nil
	}
	if interrupted && now.Before(b.ClosesAt.Add(24*time.Hour)) {
		return nil
	}
	return fmt.Errorf("batch %s closed: %w", b.ID, apperror.ErrExpired)
}

func (b ObservationBatch) Lock(now time.Time) (ObservationBatch, error) {
	if b.Status != ObservationOpen {
		return ObservationBatch{}, fmt.Errorf("batch %s cannot lock from %s: %w", b.ID, b.Status, apperror.ErrInvalidState)
	}
	b.Status = ObservationLocked
	b.Version++
	locked := now.UTC()
	b.LockedAt = &locked
	b.UpdatedAt = locked
	return b, nil
}

type Observation struct {
	ID          string    `json:"id"`
	BatchID     string    `json:"batch_id"`
	Metric      string    `json:"metric"`
	Value       float64   `json:"value"`
	Unit        string    `json:"unit"`
	ObservedAt  time.Time `json:"observed_at"`
	ReportedBy  string    `json:"reported_by"`
	Anomalous   bool      `json:"anomalous"`
	Invalidated bool      `json:"invalidated"`
	CreatedAt   time.Time `json:"created_at"`
}

func (o Observation) Validate() error {
	if o.BatchID == "" || o.ReportedBy == "" || strings.TrimSpace(o.Metric) == "" || strings.TrimSpace(o.Unit) == "" {
		return fmt.Errorf("observation identity: %w", apperror.ErrValidation)
	}
	if math.IsNaN(o.Value) || math.IsInf(o.Value, 0) {
		return fmt.Errorf("observation value: %w", apperror.ErrValidation)
	}
	if o.ObservedAt.IsZero() {
		return fmt.Errorf("observation time: %w", apperror.ErrValidation)
	}
	return nil
}

type ObservationRule struct {
	Metric string
	Unit   string
	Min    float64
	Max    float64
}

func (r ObservationRule) Evaluate(value float64) (bool, error) {
	if r.Metric == "" || r.Unit == "" || r.Min > r.Max {
		return false, fmt.Errorf("observation rule: %w", apperror.ErrValidation)
	}
	return value < r.Min || value > r.Max, nil
}
