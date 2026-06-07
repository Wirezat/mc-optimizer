package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateProductionLine inserts a new production line inside a factory.
func (d *DB) CreateProductionLine(ctx context.Context, pl *model.ProductionLine) (*model.ProductionLine, error) {
	pl.ID = uuid.New()
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO production_lines
			(id, factory_id, parent_pl_id, name,
			 target_mod_id, target_item_id,
			 rate_num, rate_den, time_unit,
			 optimize_mode, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`,
		pl.ID, pl.FactoryID, pl.ParentPLID, pl.Name,
		pl.TargetModID, pl.TargetItemID,
		pl.RateNum, pl.RateDen, pl.TimeUnit,
		pl.OptimizeMode, pl.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create production line: %w", err)
	}
	return pl, nil
}

// ListProductionLinesByFactory returns all non-archived production lines for a factory.
func (d *DB) ListProductionLinesByFactory(ctx context.Context, factoryID uuid.UUID) ([]*model.ProductionLine, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, factory_id, parent_pl_id, name,
		       target_mod_id, target_item_id,
		       rate_num, rate_den, time_unit,
		       optimize_mode, status
		FROM production_lines
		WHERE factory_id = $1 AND status != 'archived'
		ORDER BY name
	`, factoryID)
	if err != nil {
		return nil, fmt.Errorf("db: list production lines: %w", err)
	}
	defer rows.Close()

	var pls []*model.ProductionLine
	for rows.Next() {
		pl := &model.ProductionLine{}
		if err := rows.Scan(
			&pl.ID, &pl.FactoryID, &pl.ParentPLID, &pl.Name,
			&pl.TargetModID, &pl.TargetItemID,
			&pl.RateNum, &pl.RateDen, &pl.TimeUnit,
			&pl.OptimizeMode, &pl.Status,
		); err != nil {
			return nil, fmt.Errorf("db: scan production line: %w", err)
		}
		pls = append(pls, pl)
	}
	return pls, rows.Err()
}

// GetProductionLine fetches a single production line by ID.
func (d *DB) GetProductionLine(ctx context.Context, id uuid.UUID) (*model.ProductionLine, error) {
	pl := &model.ProductionLine{}
	err := d.Pool.QueryRow(ctx, `
		SELECT id, factory_id, parent_pl_id, name,
		       target_mod_id, target_item_id,
		       rate_num, rate_den, time_unit,
		       optimize_mode, status
		FROM production_lines
		WHERE id = $1
	`, id).Scan(
		&pl.ID, &pl.FactoryID, &pl.ParentPLID, &pl.Name,
		&pl.TargetModID, &pl.TargetItemID,
		&pl.RateNum, &pl.RateDen, &pl.TimeUnit,
		&pl.OptimizeMode, &pl.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get production line: %w", err)
	}
	return pl, nil
}

// ProductionLineOwnerUserID resolves the user_id that owns a production line
// via the chain production_lines → factories → saves.
func (d *DB) ProductionLineOwnerUserID(ctx context.Context, plID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT s.user_id
		FROM production_lines pl
		JOIN factories f ON f.id = pl.factory_id
		JOIN saves s ON s.id = f.save_id
		WHERE pl.id = $1
	`, plID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: production line owner: %w", err)
	}
	return userID, nil
}

// UpdateProductionLineStatus sets the status of a production line.
func (d *DB) UpdateProductionLineStatus(ctx context.Context, id uuid.UUID, status string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE production_lines SET status = $2 WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("db: update production line status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProductionLine removes a production line and cascades to pl_io and machine_groups.
func (d *DB) DeleteProductionLine(ctx context.Context, id uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM production_lines WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete production line: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreatePLIO inserts a single IO entry for a production line.
func (d *DB) CreatePLIO(ctx context.Context, io *model.PLIO) (*model.PLIO, error) {
	io.ID = uuid.New()
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO pl_io
			(id, pl_id, direction, io_type, mod_id, item_fluid_id,
			 rate_num, rate_den, is_stop_point)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,
		io.ID, io.PLID, io.Direction, io.IOType, io.ModID, io.ItemFluidID,
		io.RateNum, io.RateDen, io.IsStopPoint,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create pl_io: %w", err)
	}
	return io, nil
}

// ListPLIOByPL returns all IO entries for a production line.
func (d *DB) ListPLIOByPL(ctx context.Context, plID uuid.UUID) ([]*model.PLIO, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, pl_id, direction, io_type, mod_id, item_fluid_id,
		       rate_num, rate_den, is_stop_point
		FROM pl_io
		WHERE pl_id = $1
		ORDER BY direction, item_fluid_id
	`, plID)
	if err != nil {
		return nil, fmt.Errorf("db: list pl_io: %w", err)
	}
	defer rows.Close()

	var ios []*model.PLIO
	for rows.Next() {
		io := &model.PLIO{}
		if err := rows.Scan(
			&io.ID, &io.PLID, &io.Direction, &io.IOType, &io.ModID, &io.ItemFluidID,
			&io.RateNum, &io.RateDen, &io.IsStopPoint,
		); err != nil {
			return nil, fmt.Errorf("db: scan pl_io: %w", err)
		}
		ios = append(ios, io)
	}
	return ios, rows.Err()
}

// PLIOWithUnit extends PLIO with the time_unit from its parent production line.
type PLIOWithUnit struct {
	model.PLIO
	TimeUnit string
}

// ListActiveIOByFactory returns all IO entries for active production lines in a factory.
func (d *DB) ListActiveIOByFactory(ctx context.Context, factoryID uuid.UUID) ([]PLIOWithUnit, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT io.id, io.pl_id, io.direction, io.io_type, io.mod_id, io.item_fluid_id,
		       io.rate_num, io.rate_den, io.is_stop_point, pl.time_unit
		FROM pl_io io
		JOIN production_lines pl ON pl.id = io.pl_id
		WHERE pl.factory_id = $1 AND pl.status = 'active'
		ORDER BY io.direction, io.item_fluid_id
	`, factoryID)
	if err != nil {
		return nil, fmt.Errorf("db: list active io by factory: %w", err)
	}
	defer rows.Close()

	var result []PLIOWithUnit
	for rows.Next() {
		var r PLIOWithUnit
		if err := rows.Scan(
			&r.PLIO.ID, &r.PLIO.PLID, &r.PLIO.Direction, &r.PLIO.IOType,
			&r.PLIO.ModID, &r.PLIO.ItemFluidID,
			&r.PLIO.RateNum, &r.PLIO.RateDen, &r.PLIO.IsStopPoint,
			&r.TimeUnit,
		); err != nil {
			return nil, fmt.Errorf("db: list active io by factory: scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// CreateMachineGroup inserts a single machine group for a production line.
func (d *DB) CreateMachineGroup(ctx context.Context, mg *model.MachineGroup) (*model.MachineGroup, error) {
	mg.ID = uuid.New()
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO machine_groups
			(id, pl_id, machine_mod_id, machine_id, recipe_id,
			 count, upgrade_tier_id, upgrade_count, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,
		mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
		mg.Count, mg.UpgradeTierID, mg.UpgradeCount, mg.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create machine group: %w", err)
	}
	return mg, nil
}

// ListMachineGroupsByPL returns all machine groups for a production line.
func (d *DB) ListMachineGroupsByPL(ctx context.Context, plID uuid.UUID) ([]*model.MachineGroup, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, pl_id, machine_mod_id, machine_id, recipe_id,
		       count, upgrade_tier_id, upgrade_count, status
		FROM machine_groups
		WHERE pl_id = $1
		ORDER BY machine_id
	`, plID)
	if err != nil {
		return nil, fmt.Errorf("db: list machine groups: %w", err)
	}
	defer rows.Close()

	var groups []*model.MachineGroup
	for rows.Next() {
		mg := &model.MachineGroup{}
		if err := rows.Scan(
			&mg.ID, &mg.PLID, &mg.MachineModID, &mg.MachineID, &mg.RecipeID,
			&mg.Count, &mg.UpgradeTierID, &mg.UpgradeCount, &mg.Status,
		); err != nil {
			return nil, fmt.Errorf("db: scan machine group: %w", err)
		}
		groups = append(groups, mg)
	}
	return groups, rows.Err()
}

// UpdateMachineGroupStatus sets the status of a single machine group.
func (d *DB) UpdateMachineGroupStatus(ctx context.Context, id uuid.UUID, status string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE machine_groups SET status = $2 WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("db: update machine group status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MachineGroupOwnerUserID resolves the user_id that owns a machine group
// via the chain machine_groups → production_lines → factories → saves.
func (d *DB) MachineGroupOwnerUserID(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT s.user_id
		FROM machine_groups mg
		JOIN production_lines pl ON pl.id = mg.pl_id
		JOIN factories f ON f.id = pl.factory_id
		JOIN saves s ON s.id = f.save_id
		WHERE mg.id = $1
	`, groupID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: machine group owner: %w", err)
	}
	return userID, nil
}

// MarkAllPlannedAsBuilt sets all 'planned' machine groups in a production line to 'built'.
// Returns the number of rows updated.
func (d *DB) MarkAllPlannedAsBuilt(ctx context.Context, plID uuid.UUID) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET status = 'built'
		WHERE pl_id = $1 AND status = 'planned'
	`, plID)
	if err != nil {
		return 0, fmt.Errorf("db: mark planned as built: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ConfirmSolverDraft persists a solved production line in a single transaction:
// creates the ProductionLine, all PLIO entries, all MachineGroups, and deletes
// the consumed solver draft. IDs are assigned here; callers leave them as zero.
func (d *DB) ConfirmSolverDraft(
	ctx context.Context,
	pl *model.ProductionLine,
	ios []*model.PLIO,
	groups []*model.MachineGroup,
	draftID uuid.UUID,
) (*model.ProductionLineDetail, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: confirm solver draft: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Insert production line.
	pl.ID = uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO production_lines
			(id, factory_id, parent_pl_id, name,
			 target_mod_id, target_item_id,
			 rate_num, rate_den, time_unit,
			 optimize_mode, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, pl.ID, pl.FactoryID, pl.ParentPLID, pl.Name,
		pl.TargetModID, pl.TargetItemID,
		pl.RateNum, pl.RateDen, pl.TimeUnit,
		pl.OptimizeMode, pl.Status,
	); err != nil {
		return nil, fmt.Errorf("db: confirm solver draft: insert production line: %w", err)
	}

	// 2. Insert PLIO entries.
	for _, io := range ios {
		io.ID = uuid.New()
		io.PLID = pl.ID
		if _, err := tx.Exec(ctx, `
			INSERT INTO pl_io
				(id, pl_id, direction, io_type, mod_id, item_fluid_id,
				 rate_num, rate_den, is_stop_point)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, io.ID, io.PLID, io.Direction, io.IOType, io.ModID, io.ItemFluidID,
			io.RateNum, io.RateDen, io.IsStopPoint,
		); err != nil {
			return nil, fmt.Errorf("db: confirm solver draft: insert pl_io: %w", err)
		}
	}

	// 3. Insert machine groups.
	for _, mg := range groups {
		mg.ID = uuid.New()
		mg.PLID = pl.ID
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_groups
				(id, pl_id, machine_mod_id, machine_id, recipe_id,
				 count, upgrade_tier_id, upgrade_count, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
			mg.Count, mg.UpgradeTierID, mg.UpgradeCount, mg.Status,
		); err != nil {
			return nil, fmt.Errorf("db: confirm solver draft: insert machine group: %w", err)
		}
	}

	// 4. Delete the consumed draft — it must not be reused.
	if _, err := tx.Exec(ctx, `DELETE FROM solver_drafts WHERE id = $1`, draftID); err != nil {
		return nil, fmt.Errorf("db: confirm solver draft: delete draft: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: confirm solver draft: commit: %w", err)
	}

	return &model.ProductionLineDetail{
		ProductionLine: *pl,
		MachineGroups:  groups,
		IO:             ios,
	}, nil
}
