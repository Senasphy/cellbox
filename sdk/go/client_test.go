package cellbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecStreamAndResumeUsePersistedCursor(t *testing.T) {
	var resumeCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/ses_test/execs":
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"id":"exe_existing","stream_url":"/v1/sessions/ses_test/execs/exe_existing/stream"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/ses_test/execs/exe_existing/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			if resumeCursor = r.Header.Get("Last-Event-ID"); resumeCursor == "" {
				_, _ = fmt.Fprint(w, "id: 5\nevent: output\ndata: {\"seq\":0,\"ts\":\"2026-11-22T00:00:00Z\",\"stream\":\"stdout\",\"data\":\"aGVsbG8=\"}\n\n")
			} else {
				_, _ = fmt.Fprint(w, "id: 5\nevent: exit\ndata: {\"state\":\"exited\",\"exit_code\":0}\n\n")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "cbx_test", HTTP: server.Client()}
	var chunks []OutputChunk
	result, err := client.ExecStream(context.Background(), "ses_test", []string{"echo", "hello"}, time.Second, func(chunk OutputChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != io.ErrUnexpectedEOF {
		t.Fatal(err)
	}
	if result.ID != "exe_existing" || len(chunks) != 1 || string(chunks[0].Data) != "hello" || chunks[0].Cursor != 5 {
		t.Fatalf("stream result=%+v chunks=%+v", result, chunks)
	}
	resumed, err := client.ResumeExecStream(context.Background(), "ses_test", result.ID, chunks[0].Cursor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumeCursor != "5" || resumed.ID != result.ID || resumed.State != "exited" || resumed.ExitCode == nil || *resumed.ExitCode != 0 {
		t.Fatalf("resume cursor=%q result=%+v", resumeCursor, resumed)
	}
}

func TestExecStreamRejectsForeignStreamURL(t *testing.T) {
	client := &Client{BaseURL: "https://cellbox.example"}
	if _, err := client.streamURL("https://attacker.example/steal"); err == nil {
		t.Fatal("accepted stream URL outside configured origin")
	}
}

func TestReadFileRejectsInvalidBaseURL(t *testing.T) {
	client := &Client{BaseURL: "http://[::1]:namedport", APIKey: "cbx_test"}
	if _, err := client.ReadFile(context.Background(), "ses_test", "/tmp/a"); err == nil {
		t.Fatal("expected error for malformed BaseURL")
	}
}
