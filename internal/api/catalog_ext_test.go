package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

func TestListTagMembersHandlerGroupsByKind(t *testing.T) {
	d := deleteTestDB(t)
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx, `INSERT INTO mods (mod_id, name) VALUES ('r1apimod', 'r1apimod')`); err != nil {
		t.Fatalf("seed mod: %v", err)
	}
	t.Cleanup(func() {
		d.Pool.Exec(context.Background(), `DELETE FROM mods WHERE mod_id = 'r1apimod'`)
		d.Pool.Exec(context.Background(), `DELETE FROM tags WHERE name = 'r1apitest:honey'`)
	})
	if err := d.UpsertFluids(ctx, "r1apimod", []string{"honey"}); err != nil {
		t.Fatalf("fluid: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `INSERT INTO items (mod_id, item_id) VALUES ('r1apimod', 'honey_bottle')`); err != nil {
		t.Fatalf("item: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1apimod", model.TagKindFluid, "r1apitest:honey", []string{"r1apimod:honey"}); err != nil {
		t.Fatalf("fluid tag: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1apimod", model.TagKindItem, "r1apitest:honey", []string{"r1apimod:honey_bottle"}); err != nil {
		t.Fatalf("item tag: %v", err)
	}
	rec := httptest.NewRecorder()
	ListTagMembersHandler(d)(rec, httptest.NewRequest(http.MethodGet, "/api/tag-members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got map[string]map[string][]struct {
		ModID string `json:"mod_id"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fl, it := got["fluid"]["r1apitest:honey"], got["item"]["r1apitest:honey"]
	if len(fl) != 1 || fl[0].ID != "honey" || len(it) != 1 || it[0].ID != "honey_bottle" {
		t.Errorf("fluid = %+v, item = %+v; want honey and honey_bottle under their kind", fl, it)
	}
}
