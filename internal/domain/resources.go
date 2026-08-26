package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

type SeedLot struct {
	ID            string    `json:"id"`
	VarietyID     string    `json:"variety_id"`
	InstitutionID string    `json:"institution_id"`
	LotCode       string    `json:"lot_code"`
	QuantityGrams int64     `json:"quantity_grams"`
	ReservedGrams int64     `json:"reserved_grams"`
	ExpiresAt     time.Time `json:"expires_at"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s SeedLot) AvailableAt(now time.Time) int64 {
	if !now.Before(s.ExpiresAt) {
		return 0
	}
	available := s.QuantityGrams - s.ReservedGrams
	if available < 0 {
		return 0
	}
	return available
}

func (s SeedLot) Reserve(amount int64, now time.Time) (SeedLot, error) {
	if amount <= 0 {
		return SeedLot{}, fmt.Errorf("seed amount: %w", apperror.ErrValidation)
	}
	if !now.Before(s.ExpiresAt) {
		return SeedLot{}, fmt.Errorf("seed lot %s expired: %w", s.ID, apperror.ErrExpired)
	}
	if s.AvailableAt(now) < amount {
		return SeedLot{}, fmt.Errorf("seed lot %s has %d available: %w", s.ID, s.AvailableAt(now), apperror.ErrCapacity)
	}
	s.ReservedGrams += amount
	s.Version++
	s.UpdatedAt = now.UTC()
	return s, nil
}

func (s SeedLot) Release(amount int64, now time.Time) (SeedLot, error) {
	if amount <= 0 || amount > s.ReservedGrams {
		return SeedLot{}, fmt.Errorf("seed release amount: %w", apperror.ErrValidation)
	}
	s.ReservedGrams -= amount
	s.Version++
	s.UpdatedAt = now.UTC()
	return s, nil
}

type TrialSite struct {
	ID            string    `json:"id"`
	InstitutionID string    `json:"institution_id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Region        string    `json:"region"`
	Timezone      string    `json:"timezone"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
}

type PlotSeasonStatus string

const (
	PlotSeasonAvailable PlotSeasonStatus = "available"
	PlotSeasonReserved  PlotSeasonStatus = "reserved"
	PlotSeasonInUse     PlotSeasonStatus = "in_use"
	PlotSeasonReleased  PlotSeasonStatus = "released"
)

type PlotSeason struct {
	ID            string           `json:"id"`
	SiteID        string           `json:"site_id"`
	PlotCode      string           `json:"plot_code"`
	Season        string           `json:"season"`
	AreaSquareM   int              `json:"area_square_m"`
	Status        PlotSeasonStatus `json:"status"`
	ApplicationID string           `json:"application_id,omitempty"`
	Version       int64            `json:"version"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func (p PlotSeason) Reserve(applicationID string, now time.Time) (PlotSeason, error) {
	if p.Status != PlotSeasonAvailable && p.Status != PlotSeasonReleased {
		return PlotSeason{}, fmt.Errorf("plot season %s is %s: %w", p.ID, p.Status, apperror.ErrConflict)
	}
	if strings.TrimSpace(applicationID) == "" {
		return PlotSeason{}, fmt.Errorf("plot application: %w", apperror.ErrValidation)
	}
	p.Status = PlotSeasonReserved
	p.ApplicationID = applicationID
	p.Version++
	p.UpdatedAt = now.UTC()
	return p, nil
}

func (p PlotSeason) Release(applicationID string, now time.Time) (PlotSeason, error) {
	if p.ApplicationID != applicationID || p.Status != PlotSeasonReserved {
		return PlotSeason{}, fmt.Errorf("plot reservation ownership: %w", apperror.ErrConflict)
	}
	p.Status = PlotSeasonReleased
	p.ApplicationID = ""
	p.Version++
	p.UpdatedAt = now.UTC()
	return p, nil
}

type Allocation struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	SeedLotID     string     `json:"seed_lot_id"`
	PlotSeasonID  string     `json:"plot_season_id"`
	SeedGrams     int64      `json:"seed_grams"`
	Status        string     `json:"status"`
	AllocatedBy   string     `json:"allocated_by"`
	AllocatedAt   time.Time  `json:"allocated_at"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
}

func (a Allocation) Validate() error {
	if a.ApplicationID == "" || a.SeedLotID == "" || a.PlotSeasonID == "" || a.AllocatedBy == "" {
		return fmt.Errorf("allocation references: %w", apperror.ErrValidation)
	}
	if a.SeedGrams <= 0 {
		return fmt.Errorf("allocation seed amount: %w", apperror.ErrValidation)
	}
	return nil
}
