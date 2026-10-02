package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func modfileUpload(t *testing.T, modYML string) *http.Request {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	f, err := zw.Create("mod.yml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := f.Write([]byte(modYML)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("modfile", "broken.zip")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	part.Write(zbuf.Bytes())
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/import/modfile", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req.WithContext(context.WithValue(req.Context(), contextKeyUserID, uuid.New()))
}

func TestImportModFileHandlerInvalidModfileNamesWhere(t *testing.T) {
	d := deleteTestDB(t)
	yml := "mod_id: brokenmod\nmachines:\n  - id: m\nrecipes:\n  - machine: m\n    duration_ticks: 1\n    inputs:\n      items:\n        - item: brokenmod:dust\n          amount: 0\n"
	rec := httptest.NewRecorder()
	ImportModFileHandler(d, t.TempDir(), nil)(rec, modfileUpload(t, yml))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var got apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if got.Code != CodeModFileInvalid {
		t.Errorf("code = %q, want %q", got.Code, CodeModFileInvalid)
	}
	for _, want := range []string{"broken.zip: modfile: mod.yml line 9", "recipe on m", "input brokenmod:dust", "must be positive"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q does not name %q", got.Message, want)
		}
	}
}
