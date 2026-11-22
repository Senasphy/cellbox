package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/store"
)

type asciicastHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type asciicastEvent [3]any

func (s *server) exportAsciicast(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	exec, err := persist.GetExec(r.Context(), r.PathValue("execID"))
	if err != nil || exec.SessionID != session.ID || !exec.TTY {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "exec_not_found"})
		return
	}
	output, err := persist.ListExecOutput(r.Context(), exec.ID, 0)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "asciicast_read_failed"})
		return
	}
	w.Header().Set("Content-Type", "application/x-asciicast; version=2")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+exec.ID+".cast\"")
	data, err := encodeAsciicast(asciicastHeader{Version: 2, Width: 80, Height: 24, Timestamp: exec.StartedAt.Unix(), Env: map[string]string{"TERM": "xterm-256color"}}, exec.StartedAt, output)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "asciicast_encode_failed"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func encodeAsciicast(header asciicastHeader, startedAt time.Time, output []store.ExecOutputRecord) ([]byte, error) {
	var result []byte
	appendLine := func(value any) error {
		line, err := json.Marshal(value)
		if err != nil {
			return err
		}
		result = append(result, line...)
		result = append(result, '\n')
		return nil
	}
	if err := appendLine(header); err != nil {
		return nil, fmt.Errorf("encode asciicast header: %w", err)
	}
	for _, chunk := range output {
		if chunk.Stream != "tty" && chunk.Stream != "stdout" {
			continue
		}
		elapsed := chunk.Timestamp.Sub(startedAt).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		if err := appendLine(asciicastEvent{elapsed, "o", string(chunk.Data)}); err != nil {
			return nil, err
		}
	}
	return result, nil
}
