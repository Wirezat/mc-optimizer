package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/fracidx"
	"github.com/google/uuid"
)

// mockPositionStore is an in-memory PositionStore for testing.
type mockPositionStore struct {
	positions map[uuid.UUID]string
	owners    map[uuid.UUID]uuid.UUID
}

func newMockStore() *mockPositionStore {
	return &mockPositionStore{
		positions: make(map[uuid.UUID]string),
		owners:    make(map[uuid.UUID]uuid.UUID),
	}
}

func (m *mockPositionStore) add(id, ownerID uuid.UUID, pos string) {
	m.positions[id] = pos
	m.owners[id] = ownerID
}

func (m *mockPositionStore) OwnerUserID(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	uid, ok := m.owners[id]
	if !ok {
		return uuid.Nil, db.ErrNotFound
	}
	return uid, nil
}

func (m *mockPositionStore) GetPosition(_ context.Context, id uuid.UUID) (string, error) {
	pos, ok := m.positions[id]
	if !ok {
		return "", db.ErrNotFound
	}
	return pos, nil
}

func (m *mockPositionStore) SetPosition(_ context.Context, id uuid.UUID, position string) error {
	if _, ok := m.positions[id]; !ok {
		return db.ErrNotFound
	}
	m.positions[id] = position
	return nil
}

// withUserCtx injects a userID into the request context (same key as RequireAuth
// middleware).
func withUserCtx(r *http.Request, userID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), contextKeyUserID, userID)
	return r.WithContext(ctx)
}

// doReorder fires the ReorderHandler against a ServeMux and returns the recorder.
func doReorder(store PositionStore, idParam, entityID string, body any, userID uuid.UUID) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("PATCH /items/{"+idParam+"}/position", ReorderHandler(store, idParam))

	bodyBytes, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPatch, "/items/"+entityID+"/position", bytes.NewReader(bodyBytes))
	r = withUserCtx(r, userID)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestReorderHandler_NormalMove(t *testing.T) {
	userID := uuid.New()
	itemA := uuid.New()
	itemB := uuid.New()
	itemC := uuid.New()

	store := newMockStore()
	store.add(itemA, userID, "V0001000")
	store.add(itemB, userID, "V0002000") // B will be moved between A and C
	store.add(itemC, userID, "V0003000")

	// Move B between A and C (it's already there, but the point is the handler works)
	w := doReorder(store, "id", itemB.String(), map[string]any{
		"after_id":  itemA.String(),
		"before_id": itemC.String(),
	}, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	newPos := store.positions[itemB]
	if newPos <= "V0001000" || newPos >= "V0003000" {
		t.Errorf("new position %q not between V0001000 and V0003000", newPos)
	}
}

func TestReorderHandler_MoveToBeginning(t *testing.T) {
	userID := uuid.New()
	itemA := uuid.New()
	itemB := uuid.New()

	store := newMockStore()
	store.add(itemA, userID, "V0001000") // first
	store.add(itemB, userID, "V0002000") // B moves before A

	w := doReorder(store, "id", itemB.String(), map[string]any{
		"after_id":  nil,
		"before_id": itemA.String(),
	}, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	newPos := store.positions[itemB]
	if newPos >= "V0001000" {
		t.Errorf("move-to-beginning: new position %q not < V0001000", newPos)
	}
}

func TestReorderHandler_MoveToEnd(t *testing.T) {
	userID := uuid.New()
	itemA := uuid.New()
	itemB := uuid.New()

	store := newMockStore()
	store.add(itemA, userID, "V0001000") // B moves after A
	store.add(itemB, userID, "V0000500")

	w := doReorder(store, "id", itemB.String(), map[string]any{
		"after_id":  itemA.String(),
		"before_id": nil,
	}, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	newPos := store.positions[itemB]
	if newPos <= "V0001000" {
		t.Errorf("move-to-end: new position %q not > V0001000", newPos)
	}
}

func TestReorderHandler_BothNil(t *testing.T) {
	userID := uuid.New()
	item := uuid.New()

	store := newMockStore()
	store.add(item, userID, "someOldPos")

	w := doReorder(store, "id", item.String(), map[string]any{
		"after_id":  nil,
		"before_id": nil,
	}, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	newPos := store.positions[item]
	if newPos != fracidx.Initial() {
		t.Errorf("both nil: expected Initial()=%q, got %q", fracidx.Initial(), newPos)
	}
}

func TestReorderHandler_Forbidden(t *testing.T) {
	ownerID := uuid.New()
	otherUser := uuid.New()
	item := uuid.New()

	store := newMockStore()
	store.add(item, ownerID, "V0001000")

	w := doReorder(store, "id", item.String(), map[string]any{
		"after_id": nil, "before_id": nil,
	}, otherUser)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestReorderHandler_EntityNotFound(t *testing.T) {
	userID := uuid.New()
	store := newMockStore() // empty

	w := doReorder(store, "id", uuid.New().String(), map[string]any{
		"after_id": nil, "before_id": nil,
	}, userID)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestReorderHandler_NeighbourNotFound(t *testing.T) {
	userID := uuid.New()
	item := uuid.New()
	ghost := uuid.New() // referenced as after_id but not in store

	store := newMockStore()
	store.add(item, userID, "V0001000")

	w := doReorder(store, "id", item.String(), map[string]any{
		"after_id":  ghost.String(),
		"before_id": nil,
	}, userID)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing neighbour, got %d", w.Code)
	}
	// Position must be unchanged
	if store.positions[item] != "V0001000" {
		t.Error("position changed despite error")
	}
}

func TestReorderHandler_InvalidJSON(t *testing.T) {
	userID := uuid.New()
	item := uuid.New()

	store := newMockStore()
	store.add(item, userID, "V0001000")

	mux := http.NewServeMux()
	mux.Handle("PATCH /items/{id}/position", ReorderHandler(store, "id"))

	r := httptest.NewRequest(http.MethodPatch, "/items/"+item.String()+"/position",
		bytes.NewReader([]byte("not json {")))
	r = withUserCtx(r, userID)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReorderHandler_InvalidUUID(t *testing.T) {
	userID := uuid.New()
	store := newMockStore()

	mux := http.NewServeMux()
	mux.Handle("PATCH /items/{id}/position", ReorderHandler(store, "id"))

	r := httptest.NewRequest(http.MethodPatch, "/items/not-a-uuid/position",
		bytes.NewReader([]byte(`{"after_id":null,"before_id":null}`)))
	r = withUserCtx(r, userID)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid UUID, got %d", w.Code)
	}
}

func TestReorderHandler_PositionActuallyBetweenNeighbours(t *testing.T) {
	userID := uuid.New()

	// Simulate a list of 10 items; move item 9 between items 2 and 3
	ids := make([]uuid.UUID, 10)
	for i := range ids {
		ids[i] = uuid.New()
	}

	store := newMockStore()
	for i, id := range ids {
		store.add(id, userID, fracidx.Between(
			// Build evenly-spaced initial positions
			func() string {
				if i == 0 {
					return ""
				}
				return store.positions[ids[i-1]]
			}(),
			"",
		))
	}

	afterID := ids[2]
	beforeID := ids[3]
	movedID := ids[9]

	afterPos := store.positions[afterID]
	beforePos := store.positions[beforeID]

	w := doReorder(store, "id", movedID.String(), map[string]any{
		"after_id":  afterID.String(),
		"before_id": beforeID.String(),
	}, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	newPos := store.positions[movedID]
	if newPos <= afterPos || newPos >= beforePos {
		t.Errorf("new position %q not strictly between %q and %q", newPos, afterPos, beforePos)
	}
}
