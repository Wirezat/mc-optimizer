package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/fracidx"
	"github.com/google/uuid"
)

// PositionStore is implemented by any DB-layer wrapper that supports fractional-index
// reordering.
type PositionStore interface {
	OwnerUserID(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	GetPosition(ctx context.Context, id uuid.UUID) (string, error)
	SetPosition(ctx context.Context, id uuid.UUID, position string) error
}

// ReorderHandler returns a PATCH handler that moves an entity to a new position.
func ReorderHandler(store PositionStore, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		id, ok := parseUUIDParam(w, r, idParam)
		if !ok {
			return
		}

		ownerID, err := store.OwnerUserID(r.Context(), id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		if ownerID != userID {
			errForbidden(w)
			return
		}

		var body struct {
			AfterID  *uuid.UUID `json:"after_id"`
			BeforeID *uuid.UUID `json:"before_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}

		afterPos := ""
		if body.AfterID != nil {
			afterPos, err = store.GetPosition(r.Context(), *body.AfterID)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}
		}

		beforePos := ""
		if body.BeforeID != nil {
			beforePos, err = store.GetPosition(r.Context(), *body.BeforeID)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}
		}

		newPos := fracidx.Between(afterPos, beforePos)
		if err := store.SetPosition(r.Context(), id, newPos); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// PLPositionStore adapts db.DB for production-line reordering.
type PLPositionStore struct{ DB *db.DB }

func (s PLPositionStore) OwnerUserID(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.DB.ProductionLineOwnerUserID(ctx, id)
}

// PLReorderHandler is like ReorderHandler but also accepts after_group_id /
// before_group_id, which reference pl_groups.position in the unified list space (used when
// an ungrouped PL is positioned next to a group boundary).
func PLReorderHandler(database *db.DB) http.HandlerFunc {
	store := PLPositionStore{DB: database}
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		id, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}

		ownerID, err := store.OwnerUserID(r.Context(), id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		if ownerID != userID {
			errForbidden(w)
			return
		}

		var body struct {
			AfterID       *uuid.UUID `json:"after_id"`
			BeforeID      *uuid.UUID `json:"before_id"`
			AfterGroupID  *uuid.UUID `json:"after_group_id"`
			BeforeGroupID *uuid.UUID `json:"before_group_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}

		afterPos := ""
		switch {
		case body.AfterID != nil:
			afterPos, err = database.GetPLPosition(r.Context(), *body.AfterID)
		case body.AfterGroupID != nil:
			afterPos, err = database.GetGroupPosition(r.Context(), *body.AfterGroupID)
		}
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}

		beforePos := ""
		switch {
		case body.BeforeID != nil:
			beforePos, err = database.GetPLPosition(r.Context(), *body.BeforeID)
		case body.BeforeGroupID != nil:
			beforePos, err = database.GetGroupPosition(r.Context(), *body.BeforeGroupID)
		}
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}

		newPos := fracidx.Between(afterPos, beforePos)
		if err := database.SetPLPosition(r.Context(), id, newPos); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// GroupPositionStore adapts db.DB for PL-group reordering.
type GroupPositionStore struct{ DB *db.DB }

func (s GroupPositionStore) OwnerUserID(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.DB.PLGroupOwnerUserID(ctx, id)
}
func (s GroupPositionStore) GetPosition(ctx context.Context, id uuid.UUID) (string, error) {
	return s.DB.GetGroupPosition(ctx, id)
}
func (s GroupPositionStore) SetPosition(ctx context.Context, id uuid.UUID, position string) error {
	return s.DB.SetGroupPosition(ctx, id, position)
}
