package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImagesCommandsListPullRemove(t *testing.T) {
	t.Setenv("CELLBOX_KEY", "cbx_test_secret")
	var listed, pulled, removed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cbx_test_secret" {
			t.Errorf("missing auth: %s %s", r.Method, r.URL)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images":
			listed = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"images":[{"reference":"alpine:latest","digest":"sha256:abc","size":42}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images":
			pulled = true
			var body struct {
				Reference string `json:"reference"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Reference != "alpine:latest" {
				t.Errorf("pull body=%+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"reference":"alpine:latest","digest":"sha256:abc","size":42}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/images" && r.URL.Query().Get("reference") == "alpine:latest":
			removed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	list := runCLI(t, "images", "ls", "--url", server.URL, "--json")
	if !listed || !strings.Contains(list, "alpine:latest") || !strings.Contains(list, "sha256:abc") {
		t.Fatalf("listed=%v output=%q", listed, list)
	}
	pull := runCLI(t, "images", "pull", "alpine:latest", "--url", server.URL)
	if !pulled || !strings.Contains(pull, "sha256:abc") {
		t.Fatalf("pulled=%v output=%q", pulled, pull)
	}
	remove := runCLI(t, "images", "rm", "alpine:latest", "--url", server.URL)
	if !removed || !strings.Contains(remove, "Removed alpine:latest") {
		t.Fatalf("removed=%v output=%q", removed, remove)
	}
}

func TestImagesCommandRequiresKey(t *testing.T) {
	t.Setenv("CELLBOX_KEY", "")
	root := newRoot()
	root.SetArgs([]string{"images", "ls"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected missing key error")
	}
}
