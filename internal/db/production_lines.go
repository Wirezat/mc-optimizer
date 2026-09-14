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
		SELECT pl.id, pl.factory_id, pl.parent_pl_id,
		       pl.target_mod_id, pl.target_item_id,
		       pl.rate_num, pl.rate_den, pl.time_unit,
		       pl.optimize_mode, pl.status, pl.pl_group_id, pl.position,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||pl.target_mod_id||'.'||pl.target_item_id),
		         ''
		       ) AS target_item_name,
		       EXISTS(SELECT 1 FROM fluids WHERE mod_id = pl.target_mod_id AND fluid_id = pl.target_item_id) AS target_is_fluid,
		       -- True if any machine group's mod isn't active for this PL's save —
		       -- surfaces a warning icon without touching the PL's own data.
		       EXISTS(
		         SELECT 1 FROM machine_groups mg
		         WHERE mg.pl_id = pl.id
		           AND NOT EXISTS (
		             SELECT 1 FROM save_active_mods sam
		             WHERE sam.save_id = f.save_id AND sam.mod_id = mg.machine_mod_id
		           )
		       ) AS mod_missing
		FROM production_lines pl
		JOIN factories f ON f.id = pl.factory_id
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
			&pl.ID, &pl.FactoryID, &pl.ParentPLID,
			&pl.TargetModID, &pl.TargetItemID,
			&pl.RateNum, &pl.RateDen, &pl.TimeUnit,
			&pl.OptimizeMode, &pl.Status, &pl.PLGroupID, &pl.Position,
			&pl.TargetItemName, &pl.TargetIsFluid, &pl.ModMissing,
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
		SELECT id, factory_id, parent_pl_id,
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
		&pl.ID, &pl.FactoryID, &pl.ParentPLID,
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

// defaultVariantID is the id of the host's built-in variant, used for a group
// whose machine's mod ships no plugin.
const defaultVariantID = "default"

// variantID falls back to the host default so the NOT NULL column never sees
// an empty id from a caller that did not set one.
func variantID(id string) string {
	if id == "" {
		return defaultVariantID
	}
	return id
}

// groupModConfig returns the group's config override, or an empty object,
// which means the save-wide config applies.
func groupModConfig(mg *model.MachineGroup) []byte {
	if len(mg.ModConfig) == 0 {
		return []byte("{}")
	}
	return mg.ModConfig
}

// ListMachineGroupsByPL returns all machine groups for a production line.
func (d *DB) ListMachineGroupsByPL(ctx context.Context, plID uuid.UUID) ([]*model.MachineGroup, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, pl_id, machine_mod_id, machine_id, recipe_id,
		       count, status, mod_config, variant_id, current_variant_id,
		       exact_count_num, exact_count_den,
		       built_count
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
			&mg.Count, &mg.Status, &mg.ModConfig, &mg.VariantID, &mg.CurrentVariantID,
			&mg.ExactCountNum, &mg.ExactCountDen,
			&mg.BuiltCount,
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
		       count, status, mod_config, variant_id, current_variant_id,
		       exact_count_num, exact_count_den,
		       built_count
		FROM machine_groups
		WHERE id = $1
	`, id).Scan(
		&mg.ID, &mg.PLID, &mg.MachineModID, &mg.MachineID, &mg.RecipeID,
		&mg.Count, &mg.Status, &mg.ModConfig, &mg.VariantID, &mg.CurrentVariantID,
		&mg.ExactCountNum, &mg.ExactCountDen,
		&mg.BuiltCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get machine group: %w", err)
	}
	return mg, nil
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

// UpdateMachineGroupBuildState sets how many of a group's target machine count
// are actually standing in-game. The built variant is tracked separately, see
// UpdateMachineGroupCurrentVariant.
func (d *DB) UpdateMachineGroupBuildState(ctx context.Context, id uuid.UUID, builtCount int) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET built_count = $2
		WHERE id = $1
	`, id, builtCount)
	if err != nil {
		return fmt.Errorf("db: update machine group build state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMachineGroupCurrentVariant records which operating variant the group's
// machines are actually built to in-game.
func (d *DB) UpdateMachineGroupCurrentVariant(ctx context.Context, id uuid.UUID, variantID string) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET current_variant_id = $2
		WHERE id = $1
	`, id, variantID)
	if err != nil {
		return fmt.Errorf("db: update machine group current variant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MachineGroupOwnerUserID resolves the user_id that owns a machine group.
func (d *DB) MachineGroupOwnerUserID(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error) {
	userID, _, err := d.MachineGroupScope(ctx, groupID)
	return userID, err
}

// MachineGroupScope resolves the owning user and the save a machine group
// belongs to, via the chain machine_groups → production_lines → factories →
// saves. Returns ErrNotFound if the group does not exist.
func (d *DB) MachineGroupScope(ctx context.Context, groupID uuid.UUID) (userID, saveID uuid.UUID, err error) {
	err = d.Pool.QueryRow(ctx, `
		SELECT s.user_id, s.id
		FROM machine_groups mg
		JOIN production_lines pl ON pl.id = mg.pl_id
		JOIN factories f ON f.id = pl.factory_id
		JOIN saves s ON s.id = f.save_id
		WHERE mg.id = $1
	`, groupID).Scan(&userID, &saveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("db: machine group scope: %w", err)
	}
	return userID, saveID, nil
}

// UpdateMachineGroupVariant sets the variant a group targets together with the
// machine count that variant needs. built_count is clamped to the new count so
// the column's CHECK holds when a faster variant shrinks the group.
func (d *DB) UpdateMachineGroupVariant(ctx context.Context, id uuid.UUID, variantID string, count int, exactNum, exactDen int64) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET variant_id = $2, count = $3, exact_count_num = $4, exact_count_den = $5,
		    built_count = LEAST(built_count, $3)
		WHERE id = $1
	`, id, variantID, count, exactNum, exactDen)
	if err != nil {
		return fmt.Errorf("db: update machine group variant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllPlannedAsBuilt sets all 'planned' machine groups in a production line to 'built'
// and fills their built count to match the target — "mark all built" means the player
// finished building everything as planned. Returns the number of rows updated.
func (d *DB) MarkAllPlannedAsBuilt(ctx context.Context, plID uuid.UUID) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_groups
		SET status = 'built',
		    built_count = count,
		    current_variant_id = variant_id
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
			(id, factory_id, parent_pl_id,
			 target_mod_id, target_item_id,
			 rate_num, rate_den, time_unit,
			 optimize_mode, status, position, solve_request)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, pl.ID, pl.FactoryID, pl.ParentPLID,
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
				 count, status, mod_config, variant_id, current_variant_id,
				 exact_count_num, exact_count_den)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
			mg.Count, mg.Status, groupModConfig(mg), variantID(mg.VariantID), variantID(mg.CurrentVariantID),
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
// The line's id, position, and status are preserved. New groups are inserted as given
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
				 count, status, mod_config, variant_id, current_variant_id,
				 exact_count_num, exact_count_den)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, mg.ID, mg.PLID, mg.MachineModID, mg.MachineID, mg.RecipeID,
			mg.Count, mg.Status, groupModConfig(mg), variantID(mg.VariantID), variantID(mg.CurrentVariantID),
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

// EstimateCurrentRateFraction returns the fraction (0..1) of a production line's target
// output achievable with the groups' current build state — the minimum over machine
// groups of builtCount×speed/exactCount, since a chain's output is limited by its
// slowest link. speed is the built variant's rate relative to the target variant's,
// keyed by group id; a missing entry counts as 1.
func EstimateCurrentRateFraction(groups []*model.MachineGroup, speed map[uuid.UUID]float64) float64 {
	frac := 1.0
	for _, mg := range groups {
		// The lossless fractional machine count is the true requirement; fall back to
		// the rounded count when it is unset.
		exact := float64(mg.Count)
		if mg.ExactCountDen > 0 && mg.ExactCountNum > 0 {
			exact = float64(mg.ExactCountNum) / float64(mg.ExactCountDen)
		}
		if exact <= 0 {
			continue
		}
		g := float64(mg.BuiltCount) / exact
		if sp, ok := speed[mg.ID]; ok && sp > 0 {
			g *= sp
		}
		if g > 1 {
			g = 1 // rounding the count up can overshoot the exact requirement
		}
		if g < frac {
			frac = g
		}
	}
	return frac
}
