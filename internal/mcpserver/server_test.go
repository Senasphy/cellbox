package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBoundedExecClipsStreamsAndReportsExitMetadata(t *testing.T) {
	stdout := strings.Repeat("a", 40000)
	data := map[string]any{
		"exec":   map[string]any{"state": "timed_out", "exit_code": 124, "duration_ms": 81},
		"output": []any{map[string]any{"stream": "stdout", "data": base64.StdEncoding.EncodeToString([]byte(stdout))}},
	}
	result := boundedExec(data)
	if len(result["stdout"].(string)) > 16*1024 {
		t.Fatalf("stdout exceeds limit: %d", len(result["stdout"].(string)))
	}
	if result["timeout_or_oom_reason"] != "timeout" || result["duration_ms"] != 81 || result["exit_code"] != 124 {
		t.Fatalf("missing execution metadata: %#v", result)
	}
	if result["omitted_bytes"].(map[string]int)["stdout"] == 0 {
		t.Fatal("omitted output count was not reported")
	}
}

func TestMCPToolWorksOverProtocolClient(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing API auth")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"ses_test"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/execs"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"exec":{"id":"exe_test","state":"exited","exit_code":0,"duration_ms":3},"output":[{"stream":"stdout","data":"b2s="}]}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer api.Close()
	server, cleanup := NewWithCleanup(Config{URL: api.URL, Key: "test-key"})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = cleanup(ctx)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = server.Run(ctx, st) }()
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "run_command", Arguments: map[string]any{"command": []any{"echo", "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	if call.IsError || len(call.Content) == 0 {
		t.Fatalf("tool call failed: %#v", call)
	}
	text, ok := call.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected tool result: %#v", call.Content[0])
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "ok" || result["duration_ms"] != float64(3) {
		t.Fatalf("unexpected result: %#v", result)
	}
}
