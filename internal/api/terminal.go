package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senasphy/cellbox/internal/api/problem"
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
	if box == nil {
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": "interactive terminal is unavailable"})
		return
	}
	env, err := sessionEnvironment(record, map[string]string{"TERM": "xterm-256color"})
	if err != nil {
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": "session environment is invalid"})
		return
	}
	hub, err := s.terminalForSession(record.ID, box, ctx, env)
	if err != nil {
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": "interactive terminal is unavailable"})
		return
	}
	client, scrollback, err := hub.attach()
	if err != nil {
		_ = ws.WriteJSON(map[string]string{"type": "error", "message": "terminal already has an attached client"})
		return
	}
	defer hub.detach(client)
	if len(scrollback) > 0 {
		if err := ws.WriteMessage(websocket.BinaryMessage, scrollback); err != nil {
			return
		}
	}
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
		case chunk := <-client.output:
			if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		case <-client.done:
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
					err := hub.resize(resizeCtx, client, resize.Columns, resize.Rows)
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
			if _, err := hub.write(client, message.payload); err != nil {
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
