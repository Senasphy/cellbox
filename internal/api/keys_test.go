package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

func TestUpdateKeyQuotas(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := auth.Generate("key_admin", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{
		ID:        admin.ID,
		Name:      admin.Name,
		Prefix:    admin.Prefix,
		Hash:      admin.Hash,
		Scope:     admin.Scope,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandlerWithConfig(db, &fakeRuntime{}, "fake", config.Defaults())
	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	created := do(http.MethodPost, "/v1/keys", admin.Secret, `{"name":"worker","scope":"session"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create key status=%d body=%s", created.Code, created.Body.String())
	}
	var createdResp keyResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdResp); err != nil {
		t.Fatal(err)
	}

	// Session-scoped key cannot patch quotas.
	if got := do(http.MethodPatch, "/v1/keys/"+createdResp.ID, createdResp.Secret, `{"quotas":{"max_sessions":5}}`); got.Code != http.StatusForbidden {
		t.Fatalf("non-admin patch status=%d, want 403", got.Code)
	}

	// Negative quota rejected.
	if got := do(http.MethodPatch, "/v1/keys/"+createdResp.ID, admin.Secret, `{"quotas":{"max_sessions":-1}}`); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("negative quota status=%d, want 422", got.Code)
	}

	// Partial update preserves unspecified quota fields.
	patched := do(http.MethodPatch, "/v1/keys/"+createdResp.ID, admin.Secret, `{"quotas":{"max_sessions":42,"requests_per_second":100}}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch key status=%d body=%s", patched.Code, patched.Body.String())
	}
	var patchedResp keyResponse
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedResp); err != nil {
		t.Fatal(err)
	}
	if patchedResp.Quotas.MaxSessions != 42 || patchedResp.Quotas.MaxTotalMemoryMB != 8192 || patchedResp.Quotas.RequestsPerSecond != 100 {
		t.Fatalf("unexpected merged quotas: %+v", patchedResp.Quotas)
	}

	// Missing key returns 404.
	if got := do(http.MethodPatch, "/v1/keys/key_missing", admin.Secret, `{"quotas":{"max_sessions":1}}`); got.Code != http.StatusNotFound {
		t.Fatalf("missing key patch status=%d, want 404", got.Code)
	}

	// Revoked key returns 404.
	if got := do(http.MethodDelete, "/v1/keys/"+createdResp.ID, admin.Secret, ""); got.Code != http.StatusNoContent {
		t.Fatalf("revoke key status=%d, want 204", got.Code)
	}
	if got := do(http.MethodPatch, "/v1/keys/"+createdResp.ID, admin.Secret, `{"quotas":{"max_sessions":1}}`); got.Code != http.StatusNotFound {
		t.Fatalf("revoked key patch status=%d, want 404", got.Code)
	}
}
