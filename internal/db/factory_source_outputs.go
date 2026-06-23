package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (d *DB) UpsertFactorySourceOutput(ctx context.Context, factoryID uuid.UUID, modID, itemID string, rateNum, rateDen int, timeUnit string) (*model.FactorySource, error) {
	o := &model.FactorySource{
		ID:        uuid.New(),
		FactoryID: factoryID,
		ModID:     modID,
		ItemID:    itemID,
		RateNum:   rateNum,
		RateDen:   rateDen,
		TimeUnit:  timeUnit,
	}
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO factory_source_outputs (id, factory_id, mod_id, item_id, rate_num, rate_den, time_unit)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (factory_id, mod_id, item_id)
		DO UPDATE SET rate_num = EXCLUDED.rate_num, rate_den = EXCLUDED.rate_den, time_unit = EXCLUDED.time_unit
		RETURNING id
	`, o.ID, o.FactoryID, o.ModID, o.ItemID, o.RateNum, o.RateDen, o.TimeUnit,
	).Scan(&o.ID)
	if err != nil {
		return nil, fmt.Errorf("db: upsert factory source output: %w", err)
	}
	return o, nil
}

func (d *DB) ListFactorySourceOutputs(ctx context.Context, factoryID uuid.UUID) ([]*model.FactorySource, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, factory_id, mod_id, item_id,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||mod_id||'.'||item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||mod_id||'.'||item_id),
		         ''
		       ),
		       rate_num, rate_den, time_unit
		FROM factory_source_outputs WHERE factory_id = $1 ORDER BY mod_id, item_id`,
		factoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list factory source outputs: %w", err)
	}
	defer rows.Close()

	var outputs []*model.FactorySource
	for rows.Next() {
		o := &model.FactorySource{}
		if err := rows.Scan(&o.ID, &o.FactoryID, &o.ModID, &o.ItemID, &o.Name, &o.RateNum, &o.RateDen, &o.TimeUnit); err != nil {
			return nil, fmt.Errorf("db: scan factory source output: %w", err)
		}
		outputs = append(outputs, o)
	}
	return outputs, rows.Err()
}

func (d *DB) DeleteFactorySourceOutput(ctx context.Context, id uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM factory_source_outputs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete factory source output: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FactorySourceOutputOwnerUserID(ctx context.Context, outputID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT s.user_id FROM factory_source_outputs o
		JOIN factories f ON f.id = o.factory_id
		JOIN saves s ON s.id = f.save_id
		WHERE o.id = $1
	`, outputID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: factory source output owner: %w", err)
	}
	return userID, nil
}
