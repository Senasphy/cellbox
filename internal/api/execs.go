package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/sandboxpath"
	"github.com/senasphy/cellbox/internal/store"
)

const defaultExecTimeoutMS = 60_000
const maxExecOutputBytes = runtime.MaxExecOutputBytes
const waitResponseOutputBytes = 1 << 20

type execRequest struct {
	Argv      []string          `json:"argv,omitempty"`
	Shell     string            `json:"shell,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
	TTY       bool              `json:"tty,omitempty"`
}
type execSignalRequest struct {
	Signal int `json:"signal"`
}
type execResponse struct {
	ID              string     `json:"id"`
	SessionID       string     `json:"session_id"`
	Argv            []string   `json:"argv,omitempty"`
	Shell           string     `json:"shell,omitempty"`
	Cwd             string     `json:"cwd,omitempty"`
	State           string     `json:"state"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Signal          *int       `json:"signal,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	OutputBytes     int64      `json:"output_bytes"`
	Truncated       bool       `json:"truncated"`
	PeakMemoryBytes int64      `json:"peak_memory_bytes"`
	StreamURL       string     `json:"stream_url,omitempty"`
}
type outputEvent struct {
	Seq       int64     `json:"seq"`
	Timestamp time.Time `json:"ts"`
	Stream    string    `json:"stream"`
	Data      string    `json:"data"`
}
type waitExecResponse struct {
	Exec   execResponse  `json:"exec"`
	Output []outputEvent `json:"output"`
}

type execPersistence interface {
	CreateExec(context.Context, store.ExecRecord) error
	UpdateExec(context.Context, store.ExecRecord) error
	GetExec(context.Context, string) (store.ExecRecord, error)
	PageExecs(context.Context, string, int, *store.ListCursor) ([]store.ExecRecord, bool, error)
	AppendExecOutput(context.Context, string, string, []byte, time.Time) (store.ExecOutputRecord, error)
	ListExecOutput(context.Context, string, int64) ([]store.ExecOutputRecord, error)
}

func (s *server) createExec(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	if session.State != "ready" {
		problem.Write(w, problem.Problem{Title: "Session not ready", Status: http.StatusConflict, Code: "session_not_ready"})
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || session.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the runtime cannot reconnect to this session"})
		return
	}
	var req execRequest
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	if (len(req.Argv) == 0) == (strings.TrimSpace(req.Shell) == "") {
		problem.Write(w, problem.Problem{Title: "Invalid command", Status: http.StatusUnprocessableEntity, Code: "exec_command_invalid", Detail: "provide exactly one of argv or shell"})
		return
	}
	if req.TTY && !s.backend.Capabilities().TTYExec {
		problem.Write(w, problem.Problem{Title: "TTY unsupported", Status: http.StatusUnprocessableEntity, Code: "capability_unsupported", Detail: "TTY execs are not available on this backend"})
		return
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = defaultExecTimeoutMS
	}
	if req.TimeoutMS < 1 || req.TimeoutMS > sessionLimitMaxTimeout(s.cfg.Limits.Max.TTLS) {
		problem.Write(w, problem.Problem{Title: "Invalid timeout", Status: http.StatusUnprocessableEntity, Code: "timeout_invalid"})
		return
	}
	if err := validateEnvironment(req.Env); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid environment", Status: http.StatusUnprocessableEntity, Code: "env_invalid", Detail: err.Error()})
		return
	}
	for _, arg := range req.Argv {
		if strings.ContainsRune(arg, 0) {
			problem.Write(w, problem.Problem{Title: "Invalid command", Status: http.StatusUnprocessableEntity, Code: "exec_command_invalid", Detail: "argv values cannot contain NUL"})
			return
		}
	}
	if req.Cwd != "" {
		cwd, err := cleanExecPath(req.Cwd)
		if err != nil {
			problem.Write(w, problem.Problem{Title: "Invalid working directory", Status: http.StatusUnprocessableEntity, Code: "path_outside_sandbox", Detail: err.Error()})
			return
		}
		req.Cwd = cwd
	}
	argvJSON, _ := json.Marshal(req.Argv)
	effectiveEnv, err := sessionEnvironment(session, req.Env)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_env_invalid"})
		return
	}
	envJSON, _ := json.Marshal(effectiveEnv)
	execID, err := id.New("exe")
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
		return
	}
	now := time.Now().UTC()
	record := store.ExecRecord{ID: execID, SessionID: session.ID, ArgvJSON: argvJSON, Shell: req.Shell, Cwd: req.Cwd, EnvJSON: envJSON, TTY: req.TTY, State: "starting", StartedAt: now}
	if err = persist.CreateExec(r.Context(), record); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "exec_persist_failed"})
		return
	}
	box := resolver.SandboxByID(session.RuntimeID)
	handle, err := box.Exec(r.Context(), runtime.ExecRequest{Argv: req.Argv, Shell: req.Shell, Cwd: req.Cwd, Env: effectiveEnv, TTY: req.TTY, TimeoutMS: req.TimeoutMS, OutputLimit: maxExecOutputBytes})
	if err != nil {
		record.State = "failed"
		ended := time.Now().UTC()
		record.EndedAt = &ended
		_ = persist.UpdateExec(r.Context(), record)
		problem.Write(w, problem.Problem{Title: "Exec failed", Status: http.StatusBadGateway, Code: "exec_start_failed", Detail: err.Error()})
		return
	}
	record.State = "running"
	if err = persist.UpdateExec(r.Context(), record); err != nil {
		_ = box.Kill(context.Background())
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "exec_persist_failed"})
		return
	}
	s.execMu.Lock()
	s.activeExecs[record.ID] = handle
	s.execMu.Unlock()
	done := make(chan struct{})
	go s.collectExecOutput(persist, box, handle, record, done)
	if r.URL.Query().Get("wait") == "true" {
		select {
		case <-r.Context().Done():
			return
		case <-done:
		}
		final, err := persist.GetExec(r.Context(), execID)
		if err != nil {
			problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "exec_read_failed"})
			return
		}
		output, err := persist.ListExecOutput(r.Context(), execID, 0)
		if err != nil {
			problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "exec_read_failed"})
			return
		}
		wait := waitExecResponse{Exec: toExecResponse(final, ""), Output: encodeOutput(output, waitResponseOutputBytes)}
		if final.OutputBytes > waitResponseOutputBytes {
			wait.Exec.Truncated = true
		}
		writeJSON(w, http.StatusOK, wait)
		return
	}
	writeJSON(w, http.StatusAccepted, toExecResponse(record, "/v1/sessions/"+session.ID+"/execs/"+execID+"/stream"))
}

func sessionEnvironment(session store.SessionRecord, overrides map[string]string) (map[string]string, error) {
	env := make(map[string]string)
	if len(session.EnvJSON) > 0 {
		if err := json.Unmarshal(session.EnvJSON, &env); err != nil {
			return nil, err
		}
	}
	for key, value := range overrides {
		env[key] = value
	}
	return env, nil
}

func (s *server) collectExecOutput(persist execPersistence, box runtime.Sandbox, handle runtime.ExecHandle, record store.ExecRecord, done chan<- struct{}) {
	defer close(done)
	defer func() {
		s.execMu.Lock()
		delete(s.activeExecs, record.ID)
		s.execMu.Unlock()
	}()
	var outputErr error
	output := handle.Output()
	peakCtx, peakCancel := context.WithTimeout(context.Background(), time.Second)
	if stats, err := box.Stats(peakCtx); err == nil {
		record.PeakMemoryBytes = int64(stats.MemoryBytes)
	}
	peakCancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for output != nil {
		select {
		case chunk, open := <-output:
			if !open {
				output = nil
				continue
			}
			if outputErr == nil {
				_, outputErr = persist.AppendExecOutput(context.Background(), record.ID, chunk.Stream, chunk.Data, time.Now().UTC())
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if stats, err := box.Stats(ctx); err == nil && int64(stats.MemoryBytes) > record.PeakMemoryBytes {
				record.PeakMemoryBytes = int64(stats.MemoryBytes)
			}
			cancel()
		}
	}
	result, err := handle.Wait(context.Background())
	ended := time.Now().UTC()
	record.EndedAt = &ended
	record.OutputBytes = result.OutputBytes
	record.Truncated = result.Truncated
	record.ExitCode = &result.ExitCode
	if result.Signal != 0 {
		record.Signal = &result.Signal
	}
	if result.OOMKilled {
		record.State = "oom_killed"
		record.ExitCode = nil
	} else if result.PIDsLimitHit {
		record.State = "pids_limited"
	} else if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			record.State = "timed_out"
		} else {
			record.State = "failed"
		}
		if outputErr == nil {
			outputErr = err
		}
	} else {
		record.State = "exited"
	}
	if outputErr != nil && record.State == "exited" {
		record.State = "failed"
	}
	_ = persist.UpdateExec(context.Background(), record)
	sessionTerminated := record.State == "oom_killed" || (record.State == "timed_out" && s.backend != nil && s.backend.Capabilities().TimeoutKillsSession)
	if sessionTerminated {
		if sessions, ok := s.store.(sessionPersistence); ok {
			session, err := sessions.GetSession(context.Background(), record.SessionID)
			if err == nil {
				session.State = "failed"
				if record.State == "oom_killed" {
					session.FailureReason = "memory limit killed the sandbox process group"
				} else {
					session.FailureReason = "exec timed out and the runtime stopped the session"
				}
				session.LastActivityAt = ended.UTC()
				_ = sessions.UpdateSession(context.Background(), session)
			}
		}
	}
}

func (s *server) signalExec(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	execID := r.PathValue("execID")
	record, err := persist.GetExec(r.Context(), execID)
	if err != nil || record.SessionID != session.ID {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "exec_not_found"})
		return
	}
	if record.State != "running" {
		problem.Write(w, problem.Problem{Title: "Exec not running", Status: http.StatusConflict, Code: "exec_not_running"})
		return
	}
	var req execSignalRequest
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	if req.Signal < 1 || req.Signal > 64 {
		problem.Write(w, problem.Problem{Title: "Invalid signal", Status: http.StatusUnprocessableEntity, Code: "signal_invalid", Detail: "signal must be between 1 and 64"})
		return
	}
	s.execMu.RLock()
	handle := s.activeExecs[execID]
	s.execMu.RUnlock()
	if handle == nil {
		problem.Write(w, problem.Problem{Title: "Exec not running", Status: http.StatusConflict, Code: "exec_not_running"})
		return
	}
	if err := handle.Signal(r.Context(), req.Signal); err != nil {
		if errors.Is(err, runtime.ErrUnsupported) {
			problem.Write(w, problem.Problem{Title: "Signal unsupported", Status: http.StatusUnprocessableEntity, Code: "capability_unsupported", Detail: "the selected runtime cannot signal an individual exec"})
			return
		}
		problem.Write(w, problem.Problem{Title: "Signal failed", Status: http.StatusBadGateway, Code: "exec_signal_failed", Detail: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listExecs(w http.ResponseWriter, r *http.Request) {
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
	items, hasMore, err := persist.PageExecs(r.Context(), session.ID, limit, cursor)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "exec_list_failed"})
		return
	}
	result := make([]execResponse, 0, len(items))
	for _, v := range items {
		result = append(result, toExecResponse(v, ""))
	}
	response := map[string]any{"items": result}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		response["next_cursor"] = encodeListCursor(last.StartedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *server) getExec(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	v, err := persist.GetExec(r.Context(), r.PathValue("execID"))
	if err != nil || v.SessionID != session.ID {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "exec_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, toExecResponse(v, ""))
}

func (s *server) streamExec(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.execStore(w)
	if !ok {
		return
	}
	execID := r.PathValue("execID")
	record, err := persist.GetExec(r.Context(), execID)
	if err != nil || record.SessionID != session.ID {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "exec_not_found"})
		return
	}
	lastID := r.Header.Get("Last-Event-ID")
	var after int64
	if lastID != "" {
		after, err = strconv.ParseInt(lastID, 10, 64)
		if err != nil || after < 0 {
			problem.Write(w, problem.Problem{Title: "Invalid event cursor", Status: http.StatusBadRequest, Code: "cursor_invalid"})
			return
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Streaming unavailable", Status: http.StatusInternalServerError, Code: "stream_unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	poll := time.NewTicker(200 * time.Millisecond)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, e := persist.ListExecOutput(r.Context(), execID, after)
		if e != nil {
			return
		}
		for _, event := range events {
			end := event.Offset + int64(len(event.Data))
			if end <= after {
				continue
			}
			payload, _ := json.Marshal(outputEvent{Seq: event.Offset, Timestamp: event.Timestamp, Stream: event.Stream, Data: base64.StdEncoding.EncodeToString(event.Data)})
			if _, e = fmt.Fprintf(w, "id: %d\nevent: output\ndata: %s\n\n", end, payload); e != nil {
				return
			}
			after = end
			flusher.Flush()
		}
		record, e = persist.GetExec(r.Context(), execID)
		if e != nil {
			return
		}
		if record.State != "starting" && record.State != "running" {
			duration := int64(0)
			if record.EndedAt != nil {
				duration = record.EndedAt.Sub(record.StartedAt).Milliseconds()
			}
			event := "exit"
			if record.State == "failed" {
				event = "error"
			}
			payload, _ := json.Marshal(map[string]any{"exit_code": record.ExitCode, "signal": record.Signal, "duration_ms": duration, "truncated": record.Truncated, "state": record.State})
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", after, event, payload)
			flusher.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, e = fmt.Fprint(w, ": heartbeat\n\n"); e != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
		}
	}
}

func (s *server) authorizedSession(w http.ResponseWriter, r *http.Request) (store.KeyRecord, store.SessionRecord, bool) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return store.KeyRecord{}, store.SessionRecord{}, false
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return store.KeyRecord{}, store.SessionRecord{}, false
	}
	session, err := persist.GetSession(r.Context(), r.PathValue("id"))
	if err != nil || !canSee(key, session) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return store.KeyRecord{}, store.SessionRecord{}, false
	}
	return key, session, true
}
func (s *server) execStore(w http.ResponseWriter) (execPersistence, bool) {
	p, ok := s.store.(execPersistence)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "store_unavailable"})
		return nil, false
	}
	return p, true
}
func toExecResponse(v store.ExecRecord, url string) execResponse {
	var argv []string
	_ = json.Unmarshal(v.ArgvJSON, &argv)
	return execResponse{ID: v.ID, SessionID: v.SessionID, Argv: argv, Shell: v.Shell, Cwd: v.Cwd, State: v.State, ExitCode: v.ExitCode, Signal: v.Signal, StartedAt: v.StartedAt, EndedAt: v.EndedAt, OutputBytes: v.OutputBytes, Truncated: v.Truncated, PeakMemoryBytes: v.PeakMemoryBytes, StreamURL: url}
}
func encodeOutput(items []store.ExecOutputRecord, limit int) []outputEvent {
	result := []outputEvent{}
	var total int
	for _, v := range items {
		if total >= limit {
			break
		}
		data := v.Data
		if total+len(data) > limit {
			data = data[:limit-total]
		}
		if len(data) == 0 {
			break
		}
		result = append(result, outputEvent{Seq: v.Offset, Timestamp: v.Timestamp, Stream: v.Stream, Data: base64.StdEncoding.EncodeToString(data)})
		total += len(data)
	}
	return result
}
func sessionLimitMaxTimeout(ttl int) int {
	if ttl <= 0 {
		return defaultExecTimeoutMS
	}
	if ttl > int(^uint(0)>>1)/1000 {
		return int(^uint(0) >> 1)
	}
	return ttl * 1000
}
func cleanExecPath(value string) (string, error) {
	if err := sandboxpath.Validate(value); err != nil {
		return "", err
	}
	if !path.IsAbs(value) {
		clean := path.Clean(value)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return "", errors.New("working directory escapes sandbox")
		}
		return clean, nil
	}
	return path.Clean(value), nil
}
