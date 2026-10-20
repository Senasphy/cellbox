package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
)

type sessionSnapshot struct {
	ID        string            `json:"id"`
	Image     string            `json:"image"`
	State     string            `json:"state"`
	Limits    runtime.Limits    `json:"limits"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`
}

func (s *server) streamSessions(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Streaming unavailable", Status: http.StatusInternalServerError, Code: "stream_unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		records, _, err := persist.PageSessions(r.Context(), key.ID, key.Scope == "admin", 200, nil)
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"code": "session_list_failed"})
			_, _ = fmt.Fprintf(w, "event: problem\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		items := make([]sessionSnapshot, 0, len(records))
		for _, record := range records {
			response := toSessionResponse(record)
			items = append(items, sessionSnapshot{ID: response.ID, Image: response.Image, State: response.State, Limits: response.Limits, Labels: response.Labels, CreatedAt: response.CreatedAt})
		}
		payload, err := json.Marshal(map[string]any{"items": items})
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "event: sessions\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
