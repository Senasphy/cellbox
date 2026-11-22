package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestAttachShellStreamsInputAndOutput(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/ses_test/terminal" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cbx_test_secret" {
			t.Errorf("auth=%q", r.Header.Get("Authorization"))
		}
		conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		kind, message, err := conn.ReadMessage()
		if err != nil {
			t.Error(err)
			return
		}
		if kind != websocket.BinaryMessage {
			t.Errorf("message kind=%d", kind)
		}
		received = string(message)
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("terminal-ready")); err != nil {
			t.Error(err)
			return
		}
		kind, message, err = conn.ReadMessage()
		if err != nil {
			t.Error(err)
			return
		}
		if kind != websocket.BinaryMessage || !bytes.Equal(message, []byte{4}) {
			t.Errorf("terminal EOF frame kind=%d payload=%v", kind, message)
		}
	}))
	defer server.Close()
	var output, stderr bytes.Buffer
	if err := attachShell(context.Background(), server.Client(), server.URL, "cbx_test_secret", "ses_test", strings.NewReader("command\n"), &output, &stderr); err != nil {
		t.Fatal(err)
	}
	if received != "command\n" || output.String() != "terminal-ready" {
		t.Fatalf("input=%q output=%q stderr=%q", received, output.String(), stderr.String())
	}
}

func TestShellRejectsInvalidSessionID(t *testing.T) {
	err := attachShell(context.Background(), http.DefaultClient, "http://127.0.0.1:1", "key", "bad/id", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid session id error")
	}
}
