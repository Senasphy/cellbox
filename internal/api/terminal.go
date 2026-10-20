package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
)

const terminalMessageLimit = 64 << 10

func (s *server) sessionTerminal(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	record, err := persist.GetSession(r.Context(), r.PathValue("id"))
	if err != nil || !canSee(key, record) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return
	}
	if record.State != "ready" || record.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Conflict", Status: http.StatusConflict, Code: "session_not_ready"})
		return
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || resolver == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return
	}
	ws, err := s.terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(terminalMessageLimit)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	box := resolver.SandboxByID(record.RuntimeID)
	term, err := box.OpenTerminal(ctx, runtime.TerminalRequest{Columns: 80, Rows: 24, Env: map[string]string{"TERM": "xterm-256color"}})
	if err != nil {
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": "interactive terminal is unavailable"})
		return
	}
	defer term.Close()
	output := make(chan []byte, 16)
	streamErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, readErr := term.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				select {
				case output <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if readErr != nil {
				streamErr <- readErr
				return
			}
		}
	}()
	clientMessages := make(chan clientTerminalMessage)
	go func() {
		defer close(clientMessages)
		for {
			kind, payload, readErr := ws.ReadMessage()
			if readErr != nil {
				return
			}
			select {
			case clientMessages <- clientTerminalMessage{kind: kind, payload: payload}:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case chunk := <-output:
			if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		case readErr := <-streamErr:
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				_ = ws.WriteJSON(map[string]string{"type": "error", "message": "terminal stream ended"})
			}
			return
		case message, open := <-clientMessages:
			if !open {
				return
			}
			if message.kind == websocket.TextMessage {
				var resize struct {
					Type    string `json:"type"`
					Columns uint16 `json:"columns"`
					Rows    uint16 `json:"rows"`
				}
				if json.Unmarshal(message.payload, &resize) == nil && resize.Type == "resize" && resize.Columns > 0 && resize.Rows > 0 {
					resizeCtx, done := context.WithTimeout(ctx, 3*time.Second)
					err := term.Resize(resizeCtx, resize.Columns, resize.Rows)
					done()
					if err != nil {
						_ = ws.WriteJSON(map[string]string{"type": "error", "message": "terminal resize failed"})
						return
					}
				}
				continue
			}
			if message.kind != websocket.BinaryMessage {
				return
			}
			if _, err := term.Write(message.payload); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

type clientTerminalMessage struct {
	kind    int
	payload []byte
}
