package api

import (
	"encoding/base64"
	"net/http"
	"sort"

	"github.com/senasphy/cellbox/internal/api/problem"
)

type replayEvent struct {
	Type      string `json:"type"`
	ExecID    string `json:"exec_id"`
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream,omitempty"`
	Data      string `json:"data,omitempty"`
	Exec      any    `json:"exec,omitempty"`
}

// replaySession returns a bounded page of timestamped exec boundaries and
// output records. Output bytes are base64 encoded so binary and TTY streams
// survive replay without lossy UTF-8 conversion.
func (s *server) replaySession(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	limit, cursor, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	execs, hasMore, err := persist.PageExecs(r.Context(), session.ID, limit, cursor)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "replay_read_failed"})
		return
	}
	events := make([]replayEvent, 0, len(execs)*2)
	for _, exec := range execs {
		events = append(events, replayEvent{Type: "exec.start", ExecID: exec.ID, Timestamp: exec.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), Exec: toExecResponse(exec, "")})
		output, readErr := persist.ListExecOutput(r.Context(), exec.ID, 0)
		if readErr != nil {
			problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "replay_read_failed"})
			return
		}
		for _, item := range output {
			events = append(events, replayEvent{Type: "output", ExecID: exec.ID, Timestamp: item.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), Stream: item.Stream, Data: base64.StdEncoding.EncodeToString(item.Data)})
		}
		if exec.EndedAt != nil {
			events = append(events, replayEvent{Type: "exec.end", ExecID: exec.ID, Timestamp: exec.EndedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), Exec: toExecResponse(exec, "")})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp < events[j].Timestamp })
	response := map[string]any{"items": events}
	if hasMore && len(execs) > 0 {
		last := execs[len(execs)-1]
		response["next_cursor"] = encodeListCursor(last.StartedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, response)
}
