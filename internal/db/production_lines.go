package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListProductionLinesByFactory returns all production lines for a factory, including archived ones.
// Results are ordered by group position then by the line's own position within its group.
func (d *DB) ListProductionLinesByFactory(ctx context.Context, factoryID uuid.UUID) ([]*model.ProductionLine, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT pl.id, pl.factory_id, pl.parent_pl_id, pl.name,
		       pl.target_mod_id, pl.target_item_id,
		       pl.rate_num, pl.rate_den, pl.time_unit,
		       pl.optimize_mode, pl.status, pl.pl_group_id, pl.position,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||pl.target_mod_id||'.'||pl.target_item_id),
		         ''
		       ) AS target_item_name,
		       EXISTS(SELECT 1 FROM fluids WHERE mod_id = pl.target_mod_id AND fluid_id = pl.target_item_id) AS target_is_fluid
		FROM production_lines pl
		LEFT JOIN pl_groups g ON g.id = pl.pl_group_id
		WHERE pl.factory_id = $1
		ORDER BY COALESCE(g.position, ''), pl.position
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
			&pl.OptimizeMode, &pl.Status, &pl.PLGroupID, &pl.Position,
			&pl.TargetItemName, &pl.TargetIsFluid,
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
		       optimize_mode, status, pl_group_id, position,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||target_mod_id||'.'||target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||target_mod_id||'.'||target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||target_mod_id||'.'||target_item_id),
		         ''
		       ),
		       EXISTS(SELECT 1 FROM fluids WHERE mod_id = target_mod_id AND fluid_id = target_item_id)
		FROM production_lines
		WHERE id = $1
	`, id).Scan(
		&pl.ID, &pl.FactoryID, &pl.ParentPLID, &pl.Name,
		&pl.TargetModID, &pl.TargetItemID,
		&pl.RateNum, &pl.RateDen, &pl.TimeUnit,
		&pl.OptimizeMode, &pl.Status, &pl.PLGroupID, &pl.Position,
		&pl.TargetItemName, &pl.TargetIsFluid,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get production line: %w", err)
	}
	return pl, nil
}

// GetPLPosition returns the current position key of a production line.
func (d *DB) GetPLPosition(ctx context.Context, id uuid.UUID) (string, error) {
	var pos string
	err := d.Pool.QueryRow(ctx, `SELECT position FROM production_lines WHERE id = $1`, id).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("db: get pl position: %w", err)
	}
	return pos, nil
}

// SetPLPosition updates the position key of a production line.
func (d *DB) SetPLPosition(ctx context.Context, id uuid.UUID, position string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE production_lines SET position = $2 WHERE id = $1`,
		id, position,
	)
	if err != nil {
		return fmt.Errorf("db: set pl position: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetLastPLPosition returns the lexicographically highest position among production
// lines in the given factory+group scope, or "" if none exist.
// Pass groupID=nil to query the ungrouped lines.
func (d *DB) GetLastPLPosition(ctx context.Context, factoryID uuid.UUID, groupID *uuid.UUID) (string, error) {
	var pos string
	var err error
	if groupID == nil {
		err = d.Pool.QueryRow(ctx, `
			SELECT position FROM production_lines
			WHERE factory_id = $1 AND pl_group_id IS NULL
			ORDER BY position DESC LIMIT 1
		`, factoryID).Scan(&pos)
	} else {
		err = d.Pool.QueryRow(ctx, `
			SELECT position FROM production_lines
			WHERE factory_id = $1 AND pl_group_id = $2
			ORDER BY position DESC LIMIT 1
		`, factoryID, groupID).Scan(&pos)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("db: get last pl position: %w", err)
	}
	return pos, nil
}

// MovePLToGroup moves a production line to a different group and updates its
// position atomically. Pass groupID=nil to ungroup the line.
func (d *DB) MovePLToGroup(ctx context.Context, plID uuid.UUID, groupID *uuid.UUID, position string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE production_lines SET pl_group_id = $2, position = $3 WHERE id = $1`,
		plID, groupID, position,
	)
	if err != nil {
		return fmt.Errorf("db: move pl to group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPLGroup assigns (or clears) the group for a production line.
func (d *DB) SetPLGroup(ctx context.Context, plID uuid.UUID, groupID *uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE production_lines SET pl_group_id = $2 WHERE id = $1`,
		plID, groupID,
	)
	if err != nil {
		return fmt.Errorf("db: set pl group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

func (d *DB) RenameProductionLine(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE production_lines SET name = $2 WHERE id = $1`,
		id, name,
	)
	if err != nil {
		return fmt.Errorf("db: rename production line: %w", err)
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

// ListPLIOByPL returns all IO entries for a production line, with display names resolved.
func (d *DB) ListPLIOByPL(ctx context.Context, plID uuid.UUID) ([]*model.PLIO, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT io.id, io.pl_id, io.direction, io.io_type, io.mod_id, io.item_fluid_id,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||io.mod_id||'.'||io.item_fluid_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||io.mod_id||'.'||io.item_fluid_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||io.mod_id||'.'||io.item_fluid_id),
		         ''
		       ),
		       io.rate_num, io.rate_den, io.is_stop_point
		FROM pl_io io
		WHERE io.pl_id = $1
		ORDER BY io.direction, io.item_fluid_id
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
			&io.Name,
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
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||io.mod_id||'.'||io.item_fluid_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||io.mod_id||'.'||io.item_fluid_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||io.mod_id||'.'||io.item_fluid_id),
		         ''
		       ),
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
			&r.PLIO.ModID, &r.PLIO.ItemFluidID, &r.PLIO.Name,
			&r.PLIO.RateNum, &r.PLIO.RateDen, &r.PLIO.IsStopPoint,
			&r.TimeUnit,
		); err != nil {
			return nil, fmt.Errorf("db: list active io by factory: scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ListMachineGroupsByPL returns all machine groups for a production line.
func (d *DB) ListMachineGroupsByPL(ctx context.Context, plID uuid.UUID) ([]*model.MachineGroup, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, pl_id, machine_mod_id, machine_id, recipe_id,
		       count, upgrade_tier_id, upgrade_count, status,
		       exact_count_num, exact_count_den,
		       built_count, current_upgrade_count
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
			&mg.ExactCountNum, &mg.ExactCountDen,
			&mg.BuiltCount, &mg.CurrentUpgradeCount,
		); err != nil {
			return nil, fmt.Errorf("db: scan machine group: %w", err)
		}
		groups = append(groups, mg)
	}
	return groups, rows.Err()
}

// GetMachineGroup returns a single machine group by ID.
func (d *DB) GetMachineGroup(ctx context.Context, id uuid.UUID) (*model.MachineGroup, error) {
	mg := &model.MachineGroup{}
	err := d.Pool.QueryRow(ctx, `
		SELECT id, pl_id, machine_mod_id, machine_id, recipe_id,
		       count, upgrade_tier_id, upgrade_count, status,
		       exact_count_num, exact_count_den,
		       built_count, current_upgrade_count
		FROM machine_groups
		WHERE id = $1
	`, id).Scan(
		&mg.ID, &mg.PLID, &mg.MachineModID, &mg.MachineID, &mg.RecipeID,
		&mg.Count, &mg.UpgradeTierID, &mg.UpgradeCount, &mg.Status,
		&mg.ExactCountNum, &mg.ExactCountDen,
		&mg.BuiltCount, &mg.CurrentUpgradeCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get machine group: %w", err)
	}
	return mg, nil
}

// UpdateMachineGroupUpgrades sets the upgrade tier, upgrade count, recomputed machine count,
// and the new fractional exact count of a single group. A nil tierID clears the upgrade.
func (d *DB) UpdateMachineGroupUpgrades(ctx context.Context, id uuid.UUID, tierID *uuid.UUID, upgradeCount, machineCount int, exactNum, exactDen int64) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET upgrade_tier_id = $2, upgrade_count = $3, count = $4,
		    exact_count_num = $5, exact_count_den = $6
		WHERE id = $1
	`, id, tierID, upgradeCount, machineCount, exactNum, exactDen)
	if err != nil {
		return fmt.Errorf("db: update machine group upgrades: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

// UpdateMachineGroupBuildState sets the current (in-game) build state of a machine group —
// how many of its target count are actually standing, and how many of its target upgrade
// loadout are installed so far (same tier as upgrade_tier_id, just fewer of them) —
// independent of the group's target count/upgrade_tier_id/upgrade_count.
func (d *DB) UpdateMachineGroupBuildState(ctx context.Context, id uuid.UUID, builtCount, currentUpgradeCount int) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET built_count = $2, current_upgrade_count = $3
		WHERE id = $1
	`, id, builtCount, currentUpgradeCount)
	if err != nil {
		return fmt.Errorf("db: update machine group build state: %w", err)
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

// MarkAllPlannedAsBuilt sets all 'planned' machine groups in a production line to 'built',
// and fills their current build state (built_count, current_upgrade_count) to match the
// target — "mark all built" means the player finished building everything as planned.
// Returns the number of rows updated.
func (d *DB) MarkAllPlannedAsBuilt(ctx context.Context, plID uuid.UUID) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET status = 'built',
		    built_count = count,
		    current_upgrade_count = upgrade_count
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
			 optimize_mode, status, position, solve_request)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, pl.ID, pl.FactoryID, pl.ParentPLID, pl.Name,
		pl.TargetModID, pl.TargetItemID,
		pl.RateNum, pl.RateDen, pl.TimeUnit,
		pl.OptimizeMode, pl.Status, pl.Position, pl.SolveRequest,
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
				 count, upgrade_tier_id, upgrade_count, status,
				 exact_count_num, exact_count_den)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`, mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
			mg.Count, mg.UpgradeTierID, mg.UpgradeCount, mg.Status,
			mg.ExactCountNum, mg.ExactCountDen,
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

// GetPLSolveRequest returns the stored solver request JSON for a production line.
// Returns ErrNotFound if the line is missing; returns nil bytes if no request was stored.
func (d *DB) GetPLSolveRequest(ctx context.Context, plID uuid.UUID) ([]byte, error) {
	var raw []byte
	err := d.Pool.QueryRow(ctx, `SELECT solve_request FROM production_lines WHERE id = $1`, plID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get pl solve request: %w", err)
	}
	return raw, nil
}

// ReplaceProductionLineContents re-solves a production line in place: it updates the line's
// rate/time-unit/mode and replaces all machine groups and IO entries in a single transaction.
// The line's id, name, position, and status are preserved. New groups are inserted as given
// (caller sets status, typically "planned"). Returns the updated detail.
func (d *DB) ReplaceProductionLineContents(
	ctx context.Context,
	plID uuid.UUID,
	rateNum, rateDen int,
	timeUnit, optimizeMode string,
	ios []*model.PLIO,
	groups []*model.MachineGroup,
) (*model.ProductionLineDetail, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: replace pl contents: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
		UPDATE production_lines
		SET rate_num = $2, rate_den = $3, time_unit = $4, optimize_mode = $5
		WHERE id = $1
	`, plID, rateNum, rateDen, timeUnit, optimizeMode)
	if err != nil {
		return nil, fmt.Errorf("db: replace pl contents: update line: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM machine_groups WHERE pl_id = $1`, plID); err != nil {
		return nil, fmt.Errorf("db: replace pl contents: delete groups: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pl_io WHERE pl_id = $1`, plID); err != nil {
		return nil, fmt.Errorf("db: replace pl contents: delete io: %w", err)
	}

	for _, io := range ios {
		io.ID = uuid.New()
		io.PLID = plID
		if _, err := tx.Exec(ctx, `
			INSERT INTO pl_io
				(id, pl_id, direction, io_type, mod_id, item_fluid_id,
				 rate_num, rate_den, is_stop_point)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, io.ID, io.PLID, io.Direction, io.IOType, io.ModID, io.ItemFluidID,
			io.RateNum, io.RateDen, io.IsStopPoint,
		); err != nil {
			return nil, fmt.Errorf("db: replace pl contents: insert pl_io: %w", err)
		}
	}
	for _, mg := range groups {
		mg.ID = uuid.New()
		mg.PLID = plID
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_groups
				(id, pl_id, machine_mod_id, machine_id, recipe_id,
				 count, upgrade_tier_id, upgrade_count, status,
				 exact_count_num, exact_count_den)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`, mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
			mg.Count, mg.UpgradeTierID, mg.UpgradeCount, mg.Status,
			mg.ExactCountNum, mg.ExactCountDen,
		); err != nil {
			return nil, fmt.Errorf("db: replace pl contents: insert machine group: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: replace pl contents: commit: %w", err)
	}

	pl, err := d.GetProductionLine(ctx, plID)
	if err != nil {
		return nil, err
	}
	return &model.ProductionLineDetail{
		ProductionLine: *pl,
		MachineGroups:  groups,
		IO:             ios,
	}, nil
}
