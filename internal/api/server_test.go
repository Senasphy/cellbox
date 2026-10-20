package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
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
	cfg := config.Defaults()
	cfg.Listen = "127.0.0.1:9090"
	cfg.TLS.Cert = "/etc/cellbox/server.crt"
	cfg.TLS.Key = "/etc/cellbox/private.key"
	h := NewHandlerWithConfig(db, nil, "native", cfg)
	unauthorized := httptest.NewRecorder()
	h.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/system", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	authorized := httptest.NewRecorder()
	h.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"backend":"native"`) || !strings.Contains(authorized.Body.String(), `"doctor_status":`) {
		t.Fatalf("status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	if !strings.Contains(authorized.Body.String(), `"listen":"127.0.0.1:9090"`) || !strings.Contains(authorized.Body.String(), `"key_configured":true`) || strings.Contains(authorized.Body.String(), cfg.TLS.Key) {
		t.Fatalf("effective config missing or exposed TLS key path: %s", authorized.Body.String())
	}
	db.key.Scope = "session"
	scoped := httptest.NewRecorder()
	h = NewHandlerWithConfig(db, nil, "native", cfg)
	h.ServeHTTP(scoped, req)
	if scoped.Code != http.StatusForbidden {
		t.Fatalf("session key status=%d body=%s", scoped.Code, scoped.Body.String())
	}
}

func TestSummarizeDoctorChecks(t *testing.T) {
	for _, test := range []struct {
		name   string
		checks []string
		want   string
	}{
		{name: "ready", checks: []string{"kernel version: 6.8.0", "seccomp: available"}, want: "ready"},
		{name: "degraded", checks: []string{"kernel version: 6.8.0", "cgroup delegation: not writable (fix: configure Delegate=yes)"}, want: "degraded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := summarizeDoctor(test.checks); got != test.want {
				t.Fatalf("summarizeDoctor()=%q, want %q", got, test.want)
			}
		})
	}
}

func TestAuthenticationEnforcesPerKeyRequestRateQuota(t *testing.T) {
	key, err := auth.Generate("key_rate", "limited", "admin")
	if err != nil {
		t.Fatal(err)
	}
	db := fakeStore{key: store.KeyRecord{ID: key.ID, Prefix: key.Prefix, Hash: key.Hash, Scope: "admin", QuotasJSON: []byte(`{"requests_per_second":1}`)}}
	h := NewHandler(db, nil, "native")
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
		r.Header.Set("Authorization", "Bearer "+key.Secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if first := request(); first.Code != http.StatusOK {
		t.Fatalf("first request status=%d body=%s", first.Code, first.Body.String())
	}
	limited := request()
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "1" || !strings.Contains(limited.Body.String(), "rate_limited") {
		t.Fatalf("limited request status=%d headers=%v body=%s", limited.Code, limited.Header(), limited.Body.String())
	}
}
