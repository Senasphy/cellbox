package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionsListAndRemove(t *testing.T) {
	t.Setenv("CELLBOX_KEY", "cbx_test_secret")
	var listed, removed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cbx_test_secret" {
			t.Errorf("missing auth: %s %s", r.Method, r.URL)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
			listed = true
			if r.URL.Query().Get("limit") != "20" || r.URL.Query().Get("cursor") != "page-two" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"ses_example","image":"alpine:latest","state":"ready","created_at":"2026-10-20T10:00:00Z"}],"next_cursor":"next"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sessions/ses_example":
			removed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	list := runCLI(t, "sessions", "ls", "--url", server.URL, "--limit", "20", "--cursor", "page-two", "--json")
	if !listed || !strings.Contains(list, "ses_example") || !strings.Contains(list, "next") {
		t.Fatalf("listed=%v output=%q", listed, list)
	}
	remove := runCLI(t, "sessions", "rm", "ses_example", "--url", server.URL)
	if !removed || !strings.Contains(remove, "Destroyed ses_example") {
		t.Fatalf("removed=%v output=%q", removed, remove)
	}
}
