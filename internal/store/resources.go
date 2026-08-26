package store

import (
	"context"
	"fmt"

	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
)

func (d *DB) InsertSeedLot(ctx context.Context, executor Executor, value domain.SeedLot) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO seed_lots(
        id,variety_id,institution_id,lot_code,quantity_grams,reserved_grams,expires_at,version,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.VarietyID, value.InstitutionID, value.LotCode,
		value.QuantityGrams, value.ReservedGrams, formatTime(value.ExpiresAt), value.Version,
		formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert seed lot %s: %w", value.ID, err)
	}
	return nil
}

func scanSeedLot(scanner interface{ Scan(...any) error }) (domain.SeedLot, error) {
	var value domain.SeedLot
	var expires, created, updated string
	err := scanner.Scan(&value.ID, &value.VarietyID, &value.InstitutionID, &value.LotCode,
		&value.QuantityGrams, &value.ReservedGrams, &expires, &value.Version, &created, &updated)
	if err != nil {
		return domain.SeedLot{}, err
	}
	if value.ExpiresAt, err = parseTime(expires); err != nil {
		return domain.SeedLot{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.SeedLot{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.SeedLot{}, err
	}
	return value, nil
}

func (d *DB) GetSeedLot(ctx context.Context, executor Executor, id string) (domain.SeedLot, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanSeedLot(executor.QueryRowContext(ctx, `SELECT id,variety_id,institution_id,lot_code,
        quantity_grams,reserved_grams,expires_at,version,created_at,updated_at FROM seed_lots WHERE id=?`, id))
	if err != nil {
		return domain.SeedLot{}, translateNotFound("seed_lot", id, err)
	}
	return value, nil
}

func (d *DB) UpdateSeedReservation(ctx context.Context, executor Executor, value domain.SeedLot, expectedVersion int64) error {
	if executor == nil {
		executor = d.sql
	}
	result, err := executor.ExecContext(ctx, `UPDATE seed_lots SET reserved_grams=?,version=?,updated_at=?
        WHERE id=? AND version=? AND quantity_grams>=?`, value.ReservedGrams, value.Version,
		formatTime(value.UpdatedAt), value.ID, expectedVersion, value.ReservedGrams)
	if err != nil {
		return fmt.Errorf("update seed reservation %s: %w", value.ID, err)
	}
	return expectOne(result, "seed_lot", value.ID)
}

func (d *DB) InsertTrialSite(ctx context.Context, executor Executor, value domain.TrialSite) error {
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO trial_sites(id,institution_id,code,name,region,timezone,active,created_at)
        VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.InstitutionID, value.Code, value.Name, value.Region,
		value.Timezone, boolInt(value.Active), formatTime(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert trial site %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) GetTrialSite(ctx context.Context, executor Executor, id string) (domain.TrialSite, error) {
	if executor == nil {
		executor = d.sql
	}
	var value domain.TrialSite
	var active int
	var created string
	err := executor.QueryRowContext(ctx, `SELECT id,institution_id,code,name,region,timezone,active,created_at FROM trial_sites WHERE id=?`, id).
		Scan(&value.ID, &value.InstitutionID, &value.Code, &value.Name, &value.Region, &value.Timezone, &active, &created)
	if err != nil {
		return domain.TrialSite{}, translateNotFound("trial_site", id, err)
	}
	value.Active = active == 1
	value.CreatedAt, err = parseTime(created)
	return value, err
}

func (d *DB) InsertPlotSeason(ctx context.Context, executor Executor, value domain.PlotSeason) error {
	if executor == nil {
		executor = d.sql
	}
	var application any
	if value.ApplicationID != "" {
		application = value.ApplicationID
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO plot_seasons(
        id,site_id,plot_code,season,area_square_m,status,application_id,version,created_at,updated_at
    ) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ID, value.SiteID, value.PlotCode, value.Season, value.AreaSquareM,
		value.Status, application, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert plot season %s: %w", value.ID, err)
	}
	return nil
}

func scanPlotSeason(scanner interface{ Scan(...any) error }) (domain.PlotSeason, error) {
	var value domain.PlotSeason
	var application *string
	var status, created, updated string
	err := scanner.Scan(&value.ID, &value.SiteID, &value.PlotCode, &value.Season, &value.AreaSquareM,
		&status, &application, &value.Version, &created, &updated)
	if err != nil {
		return domain.PlotSeason{}, err
	}
	value.Status = domain.PlotSeasonStatus(status)
	if application != nil {
		value.ApplicationID = *application
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return domain.PlotSeason{}, err
	}
	if value.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.PlotSeason{}, err
	}
	return value, nil
}

func (d *DB) GetPlotSeason(ctx context.Context, executor Executor, id string) (domain.PlotSeason, error) {
	if executor == nil {
		executor = d.sql
	}
	value, err := scanPlotSeason(executor.QueryRowContext(ctx, `SELECT id,site_id,plot_code,season,area_square_m,
        status,application_id,version,created_at,updated_at FROM plot_seasons WHERE id=?`, id))
	if err != nil {
		return domain.PlotSeason{}, translateNotFound("plot_season", id, err)
	}
	return value, nil
}

func (d *DB) UpdatePlotSeason(ctx context.Context, executor Executor, value domain.PlotSeason, expectedVersion int64) error {
	if executor == nil {
		executor = d.sql
	}
	var application any
	if value.ApplicationID != "" {
		application = value.ApplicationID
	}
	result, err := executor.ExecContext(ctx, `UPDATE plot_seasons SET status=?,application_id=?,version=?,updated_at=?
        WHERE id=? AND version=?`, value.Status, application, value.Version, formatTime(value.UpdatedAt), value.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update plot season %s: %w", value.ID, err)
	}
	return expectOne(result, "plot_season", value.ID)
}

func (d *DB) InsertAllocation(ctx context.Context, executor Executor, value domain.Allocation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if executor == nil {
		executor = d.sql
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO allocations(
        id,application_id,seed_lot_id,plot_season_id,seed_grams,status,allocated_by,allocated_at,released_at
    ) VALUES(?,?,?,?,?,?,?,?,?)`, value.ID, value.ApplicationID, value.SeedLotID, value.PlotSeasonID,
		value.SeedGrams, value.Status, value.AllocatedBy, formatTime(value.AllocatedAt), nil)
	if err != nil {
		return fmt.Errorf("insert allocation %s: %w", value.ID, err)
	}
	return nil
}

func (d *DB) GetAllocationByApplication(ctx context.Context, executor Executor, applicationID string) (domain.Allocation, error) {
	if executor == nil {
		executor = d.sql
	}
	var value domain.Allocation
	var allocated string
	var released *string
	err := executor.QueryRowContext(ctx, `SELECT id,application_id,seed_lot_id,plot_season_id,seed_grams,status,
        allocated_by,allocated_at,released_at FROM allocations WHERE application_id=?`, applicationID).Scan(
		&value.ID, &value.ApplicationID, &value.SeedLotID, &value.PlotSeasonID, &value.SeedGrams,
		&value.Status, &value.AllocatedBy, &allocated, &released)
	if err != nil {
		return domain.Allocation{}, translateNotFound("allocation", applicationID, err)
	}
	value.AllocatedAt, err = parseTime(allocated)
	if err != nil {
		return domain.Allocation{}, err
	}
	if released != nil {
		parsed, err := parseTime(*released)
		if err != nil {
			return domain.Allocation{}, err
		}
		value.ReleasedAt = &parsed
	}
	return value, nil
}
