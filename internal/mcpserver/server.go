// Package mcpserver exposes the current cellbox API as MCP tools.
package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	URL, Key, Image string
	MemoryMB        int
}
type service struct {
	cfg       Config
	mu        sync.Mutex
	sessionID string
}

func New(cfg Config) *mcp.Server {
	s, _ := newService(cfg)
	return s
}
func NewWithCleanup(cfg Config) (*mcp.Server, func(context.Context) error) { return newService(cfg) }
func newService(cfg Config) (*mcp.Server, func(context.Context) error) {
	if cfg.URL == "" {
		cfg.URL = "http://127.0.0.1:8080"
	}
	if cfg.Image == "" {
		cfg.Image = "alpine:latest"
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 512
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "cellbox", Version: "0.1.0"}, nil)
	svc := &service{cfg: cfg}
	type commandArgs struct {
		Command  []string `json:"command" jsonschema:"argv to execute"`
		TimeoutS int      `json:"timeout_s,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "run_command", Description: "Run argv inside the cellbox sandbox"}, func(ctx context.Context, _ *mcp.CallToolRequest, a commandArgs) (*mcp.CallToolResult, any, error) {
		id, e := svc.ensure(ctx)
		if e != nil {
			return nil, nil, e
		}
		if a.TimeoutS <= 0 {
			a.TimeoutS = 60
		}
		b, _ := json.Marshal(map[string]any{"argv": a.Command, "timeout_ms": a.TimeoutS * 1000})
		var out map[string]any
		e = svc.request(ctx, "POST", "/v1/sessions/"+url.PathEscape(id)+"/execs?wait=true", b, &out)
		if e != nil {
			return nil, nil, e
		}
		return result(boundedExec(out)), nil, nil
	})
	type fileArgs struct {
		Path     string `json:"path"`
		Contents string `json:"contents,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "write_file", Description: "Write UTF-8 contents to a sandbox file"}, func(ctx context.Context, _ *mcp.CallToolRequest, a fileArgs) (*mcp.CallToolResult, any, error) {
		id, e := svc.ensure(ctx)
		if e == nil {
			e = svc.writeFile(ctx, id, a.Path, a.Contents)
		}
		if e != nil {
			return nil, nil, e
		}
		return result(map[string]any{"written": true, "path": a.Path}), nil, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "read_file", Description: "Read a sandbox file"}, func(ctx context.Context, _ *mcp.CallToolRequest, a fileArgs) (*mcp.CallToolResult, any, error) {
		id, e := svc.ensure(ctx)
		var data []byte
		if e == nil {
			data, e = svc.raw(ctx, "GET", "/v1/sessions/"+url.PathEscape(id)+"/files?path="+url.QueryEscape(a.Path), nil)
		}
		if e != nil {
			return nil, nil, e
		}
		return result(map[string]any{"path": a.Path, "contents": string(data)}), nil, nil
	})
	type listArgs struct {
		Path  string `json:"path,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "list_files", Description: "List sandbox directory entries"}, func(ctx context.Context, _ *mcp.CallToolRequest, a listArgs) (*mcp.CallToolResult, any, error) {
		id, e := svc.ensure(ctx)
		if a.Path == "" {
			a.Path = "/"
		}
		if a.Limit <= 0 {
			a.Limit = 50
		}
		var out map[string]any
		if e == nil {
			e = svc.request(ctx, "GET", "/v1/sessions/"+url.PathEscape(id)+"/files/list?path="+url.QueryEscape(a.Path)+"&limit="+fmt.Sprint(a.Limit), nil, &out)
		}
		if e != nil {
			return nil, nil, e
		}
		return result(out), nil, nil
	})
	type empty struct{}
	mcp.AddTool(s, &mcp.Tool{Name: "reset", Description: "Destroy and recreate this MCP process sandbox"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
		svc.mu.Lock()
		old := svc.sessionID
		svc.sessionID = ""
		svc.mu.Unlock()
		if old != "" {
			_ = svc.request(ctx, "DELETE", "/v1/sessions/"+url.PathEscape(old), nil, nil)
		}
		id, e := svc.ensure(ctx)
		if e != nil {
			return nil, nil, e
		}
		return result(map[string]any{"session_id": id}), nil, nil
	})
	for _, name := range []string{"snapshot", "fork"} {
		n := name
		mcp.AddTool[empty, any](s, &mcp.Tool{Name: n, Description: n + " support is planned for Phase 6"}, func(context.Context, *mcp.CallToolRequest, empty) (*mcp.CallToolResult, any, error) {
			return nil, nil, fmt.Errorf("%s is not implemented by this server yet", n)
		})
	}
	cleanup := func(ctx context.Context) error {
		svc.mu.Lock()
		id := svc.sessionID
		svc.sessionID = ""
		svc.mu.Unlock()
		if id == "" {
			return nil
		}
		return svc.request(ctx, "DELETE", "/v1/sessions/"+url.PathEscape(id), nil, nil)
	}
	return s, cleanup
}
func result(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
func (s *service) ensure(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID != "" {
		return s.sessionID, nil
	}
	b, _ := json.Marshal(map[string]any{"image": s.cfg.Image, "limits": map[string]int{"memory_mb": s.cfg.MemoryMB, "cpu_millicores": 1000, "pids": 128, "ttl_s": 3600}, "network_policy": map[string]string{"mode": "none"}})
	var r struct {
		ID string `json:"id"`
	}
	if e := s.request(ctx, "POST", "/v1/sessions", b, &r); e != nil {
		return "", e
	}
	s.sessionID = r.ID
	return r.ID, nil
}
func (s *service) writeFile(ctx context.Context, id, path, contents string) error {
	req, e := http.NewRequestWithContext(ctx, "PUT", strings.TrimRight(s.cfg.URL, "/")+"/v1/sessions/"+url.PathEscape(id)+"/files?path="+url.QueryEscape(path), strings.NewReader(contents))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Key)
	req.Header.Set("Content-Type", "application/octet-stream")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(res.Body)
		return fmt.Errorf("cellbox API %d: %s", res.StatusCode, data)
	}
	return nil
}
func (s *service) request(ctx context.Context, m, p string, b []byte, out any) error {
	var data []byte
	var e error
	if b == nil {
		data, e = s.raw(ctx, m, p, nil)
	} else {
		data, e = s.raw(ctx, m, p, b)
	}
	if e != nil {
		return e
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
func (s *service) raw(ctx context.Context, m, p string, b []byte) ([]byte, error) {
	var body io.Reader
	if b != nil {
		body = strings.NewReader(string(b))
	}
	req, e := http.NewRequestWithContext(ctx, m, strings.TrimRight(s.cfg.URL, "/")+p, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Key)
	if len(b) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if m == "POST" && len(b) > 0 {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("mcp-%d", time.Now().UnixNano()))
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	data, e := io.ReadAll(res.Body)
	if e != nil {
		return nil, e
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("cellbox API %d: %s", res.StatusCode, data)
	}
	return data, nil
}
func DecodeOutput(s string) []byte { b, _ := base64.StdEncoding.DecodeString(s); return b }

func boundedExec(v map[string]any) map[string]any {
	exec, _ := v["exec"].(map[string]any)
	var stdout, stderr []byte
	if events, ok := v["output"].([]any); ok {
		for _, raw := range events {
			event, _ := raw.(map[string]any)
			data, _ := event["data"].(string)
			decoded, _ := base64.StdEncoding.DecodeString(data)
			if event["stream"] == "stderr" {
				stderr = append(stderr, decoded...)
			} else {
				stdout = append(stdout, decoded...)
			}
		}
	}
	out, outOmitted := clip(stdout, 16*1024)
	err, errOmitted := clip(stderr, 16*1024)
	state, _ := exec["state"].(string)
	var reason any
	if state == "timed_out" {
		reason = "timeout"
	} else if state == "oom" {
		reason = "oom"
	}
	duration := exec["duration_ms"]
	if duration == nil {
		start, e1 := time.Parse(time.RFC3339Nano, fmt.Sprint(exec["started_at"]))
		end, e2 := time.Parse(time.RFC3339Nano, fmt.Sprint(exec["ended_at"]))
		if e1 == nil && e2 == nil {
			duration = end.Sub(start).Milliseconds()
		} else {
			duration = int64(0)
		}
	}
	return map[string]any{"exit_code": exec["exit_code"], "duration_ms": duration, "timeout_or_oom_reason": reason,
		"stdout": out, "stderr": err, "omitted_bytes": map[string]int{"stdout": outOmitted, "stderr": errOmitted}}
}

func clip(data []byte, cap int) (string, int) {
	if len(data) <= cap {
		return string(data), 0
	}
	marker := []byte(fmt.Sprintf("\n... <omitted %d bytes> ...\n", len(data)-cap))
	room := cap - len(marker)
	if room < 0 {
		room = 0
	}
	head, tail := room/2, room-room/2
	clipped := append([]byte{}, data[:head]...)
	clipped = append(clipped, marker...)
	if tail > 0 {
		clipped = append(clipped, data[len(data)-tail:]...)
	}
	return string(clipped), len(data) - head - tail
}
