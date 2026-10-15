package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestKeysCreateListAndRevoke(t *testing.T) {
	dir := t.TempDir()
	secret := strings.TrimSpace(runCLI(t, "keys", "create", "test-key", "--data-dir", dir))
	if !strings.HasPrefix(secret, "cbx_") {
		t.Fatalf("unexpected key %q", secret)
	}
	listed := runCLI(t, "keys", "ls", "--data-dir", dir)
	if !strings.Contains(listed, "test-key") || strings.Contains(listed, secret) {
		t.Fatalf("unexpected key listing %q", listed)
	}
	fields := strings.Fields(listed)
	if len(fields) < 1 {
		t.Fatalf("missing key id: %q", listed)
	}
	runCLI(t, "keys", "revoke", fields[0], "--data-dir", dir)
	listed = runCLI(t, "keys", "ls", "--data-dir", dir)
	if !strings.Contains(listed, "revoked") {
		t.Fatalf("key was not revoked: %q", listed)
	}
}

func TestRunCreatesStreamsAndDestroysSession(t *testing.T) {
	t.Setenv("CELLBOX_KEY", "cbx_test_secret")
	var created, started, streamed, destroyed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cbx_test_secret" {
			t.Errorf("missing bearer key on %s %s", r.Method, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			created = true
			var body struct {
				Image   string            `json:"image"`
				Network map[string]string `json:"network_policy"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Image != "alpine:latest" || body.Network["mode"] != "none" {
				t.Errorf("create body=%+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ses_test"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/ses_test/execs":
			started = true
			var body struct {
				Argv []string `json:"argv"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if strings.Join(body.Argv, " ") != "echo hello" {
				t.Errorf("argv=%v", body.Argv)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"exe_test","stream_url":"/v1/sessions/ses_test/execs/exe_test/stream"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/ses_test/execs/exe_test/stream":
			streamed = true
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: output\ndata: {\"stream\":\"stdout\",\"data\":\"aGVsbG8K\"}\n\nevent: exit\ndata: {\"exit_code\":0}\n\n"))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sessions/ses_test":
			destroyed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"run", "--image", "alpine:latest", "--url", server.URL, "--", "echo", "hello"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !created || !started || !streamed || !destroyed {
		t.Fatalf("lifecycle create=%v start=%v stream=%v destroy=%v", created, started, streamed, destroyed)
	}
	if out.String() != "hello\n" {
		t.Fatalf("output=%q", out.String())
	}
}
