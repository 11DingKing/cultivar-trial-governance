package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
)

func TestSeedLotReserveAndReleasePreserveCapacity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	lot := SeedLot{ID: "lot", QuantityGrams: 1000, ReservedGrams: 200, ExpiresAt: now.Add(24 * time.Hour), Version: 4, UpdatedAt: now}
	if available := lot.AvailableAt(now); available != 800 {
		t.Fatalf("available = %d, want 800", available)
	}
	reserved, err := lot.Reserve(500, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reserved.ReservedGrams != 700 || reserved.AvailableAt(now) != 300 || reserved.Version != 5 {
		t.Fatalf("unexpected reserved lot: %+v", reserved)
	}
	released, err := reserved.Release(250, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if released.ReservedGrams != 450 || released.Version != 6 {
		t.Fatalf("unexpected released lot: %+v", released)
	}
	if lot.ReservedGrams != 200 {
		t.Fatalf("value receiver must not mutate original: %+v", lot)
	}
}

func TestSeedLotRejectsExpiredAndOverCapacityReservations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	lot := SeedLot{ID: "lot", QuantityGrams: 500, ReservedGrams: 450, ExpiresAt: now.Add(time.Hour)}
	if _, err := lot.Reserve(51, now); !errors.Is(err, apperror.ErrCapacity) {
		t.Fatalf("error = %v, want capacity", err)
	}
	if _, err := lot.Reserve(0, now); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
	if _, err := lot.Reserve(1, lot.ExpiresAt); !errors.Is(err, apperror.ErrExpired) {
		t.Fatalf("error = %v, want expired", err)
	}
	if available := lot.AvailableAt(lot.ExpiresAt); available != 0 {
		t.Fatalf("expired lot available = %d", available)
	}
}

func TestSeedLotReleaseRejectsInvalidOwnershipAmount(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	lot := SeedLot{ReservedGrams: 100}
	for _, amount := range []int64{-1, 0, 101, 1000} {
		if _, err := lot.Release(amount, now); !errors.Is(err, apperror.ErrValidation) {
			t.Fatalf("amount %d: error = %v", amount, err)
		}
	}
}

func TestPlotSeasonReservationHasExplicitOwner(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	plot := PlotSeason{ID: "plot-season", Status: PlotSeasonAvailable, Version: 1}
	reserved, err := plot.Reserve("application-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Status != PlotSeasonReserved || reserved.ApplicationID != "application-a" || reserved.Version != 2 {
		t.Fatalf("unexpected reservation: %+v", reserved)
	}
	if _, err := reserved.Reserve("application-b", now); !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("second reservation error = %v", err)
	}
	if _, err := reserved.Release("application-b", now); !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("foreign release error = %v", err)
	}
	released, err := reserved.Release("application-a", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != PlotSeasonReleased || released.ApplicationID != "" || released.Version != 3 {
		t.Fatalf("unexpected release: %+v", released)
	}
}

func TestPlotSeasonReleasedCanBeReservedAgain(t *testing.T) {
	t.Parallel()
	plot := PlotSeason{ID: "plot", Status: PlotSeasonReleased, Version: 8}
	reserved, err := plot.Reserve("next-application", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if reserved.ApplicationID != "next-application" || reserved.Version != 9 {
		t.Fatalf("unexpected re-reservation: %+v", reserved)
	}
}

func TestAllocationValidationRequiresAllResources(t *testing.T) {
	t.Parallel()
	valid := Allocation{ApplicationID: "a", SeedLotID: "s", PlotSeasonID: "p", SeedGrams: 100, AllocatedBy: "u"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Allocation){
		func(a *Allocation) { a.ApplicationID = "" },
		func(a *Allocation) { a.SeedLotID = "" },
		func(a *Allocation) { a.PlotSeasonID = "" },
		func(a *Allocation) { a.AllocatedBy = "" },
		func(a *Allocation) { a.SeedGrams = 0 },
	}
	for _, edit := range mutations {
		value := valid
		edit(&value)
		if !errors.Is(value.Validate(), apperror.ErrValidation) {
			t.Fatalf("expected invalid allocation: %+v", value)
		}
	}
}
