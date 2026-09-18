package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/db"
)

func TestSplitCatalogRef(t *testing.T) {
	cases := []struct {
		in      string
		wantMod string
		wantID  string
	}{
		{"mi:compressor", "mi", "compressor"},
		{"foo", "", ""},
		{"", "", ""},
		{"  mi:compressor  ", "mi", "compressor"},
	}
	for _, c := range cases {
		gotMod, gotID := splitCatalogRef(c.in)
		if gotMod != c.wantMod || gotID != c.wantID {
			t.Errorf("splitCatalogRef(%q) = (%q, %q), want (%q, %q)", c.in, gotMod, gotID, c.wantMod, c.wantID)
		}
	}
}

// A malformed producedByMachine value (no colon) must 400 before ever reaching the DB, for
// both SearchItemsHandler and SearchFluidsHandler.
func TestSearchItemsHandler_ProducedByMachineMalformed(t *testing.T) {
	handler := SearchItemsHandler(&db.DB{}, "")
	req := httptest.NewRequest(http.MethodGet, "/api/items?producedByMachine=foo", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed producedByMachine, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSearchFluidsHandler_ProducedByMachineMalformed(t *testing.T) {
	handler := SearchFluidsHandler(&db.DB{}, "")
	req := httptest.NewRequest(http.MethodGet, "/api/fluids?producedByMachine=foo", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed producedByMachine, got %d: %s", rec.Code, rec.Body.String())
	}
}
