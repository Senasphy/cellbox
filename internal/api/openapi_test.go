package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

func TestOpenAPIResponses(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join(filepath.Dir(source), "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}

	db, err := sqlite.Open(filepath.Join(t.TempDir(), "contract.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_contract", "contract", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateKey(context.Background(), sqlite.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithConfig(db, &fakeRuntime{}, "fake", config.Defaults())

	check := func(method, target, body string, authKey bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = "127.0.0.1:8080"
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if authKey {
			req.Header.Set("Authorization", "Bearer "+key.Secret)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		route, pathParams, err := router.FindRoute(req)
		if err != nil {
			t.Fatalf("route %s %s: %v", method, target, err)
		}
		input := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route},
			Status:                 rec.Code, Header: rec.Header(), Body: io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		}
		if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
			t.Errorf("response %s %s (%d): %v; body=%s", method, target, rec.Code, err, rec.Body.String())
		}
		return rec
	}

	check(http.MethodGet, "/healthz", "", false)
	check(http.MethodGet, "/v1/system", "", false)
	created := check(http.MethodPost, "/v1/sessions", `{"image":"python:3.12","network_policy":{"mode":"none"}}`, true)
	var session SessionResponse
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.ID == "" {
		t.Fatal("create returned empty session id")
	}
	check(http.MethodGet, "/v1/sessions", "", true)
	check(http.MethodGet, "/v1/sessions/"+session.ID, "", true)
	check(http.MethodPost, "/v1/sessions/"+session.ID+"/execs?wait=true", `{"argv":["echo","hi"]}`, true)
	check(http.MethodGet, "/v1/sessions/"+session.ID+"/files/list", "", true)
	check(http.MethodDelete, "/v1/sessions/"+session.ID, "", true)
}
