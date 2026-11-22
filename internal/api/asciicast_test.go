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
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

type ttyFakeRuntime struct{ *fakeRuntime }

func (*ttyFakeRuntime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{TTY: true, TTYExec: true}
}

func TestEncodeAsciicastV2(t *testing.T) {
	start := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	encoded, err := encodeAsciicast(asciicastHeader{Version: 2, Width: 80, Height: 24}, start, []store.ExecOutputRecord{
		{Stream: "stdout", Data: []byte("hello\r\n"), Timestamp: start.Add(250 * time.Millisecond)},
		{Stream: "stderr", Data: []byte("excluded"), Timestamp: start.Add(time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(encoded)), "\n")
	if len(lines) != 2 {
		t.Fatalf("record lines=%q", lines)
	}
	var header asciicastHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil || header.Version != 2 || header.Width != 80 || header.Height != 24 {
		t.Fatalf("header=%+v err=%v", header, err)
	}
	var event []any
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil || len(event) != 3 || event[0] != 0.25 || event[1] != "o" || event[2] != "hello\r\n" {
		t.Fatalf("event=%v err=%v", event, err)
	}
}

func TestAsciicastExportRequiresTTYExecAndReturnsV2(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "asciicast.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_cast", "cast", "session")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = db.CreateKey(context.Background(), store.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	session := store.SessionRecord{ID: "ses_cast", OwnerKeyID: key.ID, ImageReference: "alpine", RuntimeID: "container-cast", State: "ready", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	exec := store.ExecRecord{ID: "exe_cast", SessionID: session.ID, ArgvJSON: []byte(`[]`), EnvJSON: []byte(`{}`), TTY: true, State: "exited", StartedAt: now}
	if err = db.CreateExec(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendExecOutput(context.Background(), exec.ID, "stdout", []byte("hello\r\n"), now.Add(250*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithConfig(db, &ttyFakeRuntime{fakeRuntime: &fakeRuntime{}}, "fake", config.Defaults())
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+session.ID+"/execs/"+exec.ID+"/asciicast", nil)
	request.Header.Set("Authorization", "Bearer "+key.Secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/x-asciicast; version=2" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"version":2`) || !strings.Contains(lines[1], `"o","hello\r\n"`) {
		t.Fatalf("invalid asciicast v2 document: %q", response.Body.String())
	}
}
