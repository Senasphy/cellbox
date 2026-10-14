package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/store"
)

type fakeStore struct {
	key     store.KeyRecord
	pingErr error
}

func (f fakeStore) Ping(context.Context) error { return f.pingErr }
func (f fakeStore) FindKeyByPrefix(_ context.Context, p string) (store.KeyRecord, error) {
	if p != f.key.Prefix {
		return store.KeyRecord{}, errors.New("not found")
	}
	return f.key, nil
}

func TestHealthAndReady(t *testing.T) {
	h := NewHandler(fakeStore{}, nil, "native")
	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s status=%d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	NewHandler(fakeStore{pingErr: errors.New("db down")}, nil, "native").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "not_ready") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSystemRequiresValidAdminKey(t *testing.T) {
	key, err := auth.Generate("key_1", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	db := fakeStore{key: store.KeyRecord{ID: key.ID, Prefix: key.Prefix, Hash: key.Hash, Scope: "admin"}}
	h := NewHandler(db, nil, "native")
	unauthorized := httptest.NewRecorder()
	h.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/system", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	authorized := httptest.NewRecorder()
	h.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"backend":"native"`) {
		t.Fatalf("status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	db.key.Scope = "session"
	scoped := httptest.NewRecorder()
	h = NewHandler(db, nil, "native")
	h.ServeHTTP(scoped, req)
	if scoped.Code != http.StatusForbidden {
		t.Fatalf("session key status=%d body=%s", scoped.Code, scoped.Body.String())
	}
}
