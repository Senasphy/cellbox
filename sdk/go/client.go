// Package cellbox is a small typed client for the cellbox HTTP API.
package cellbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL, APIKey string
	HTTP            *http.Client
}
type ProblemError struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (e *ProblemError) Error() string {
	return fmt.Sprintf("cellbox %s (%d): %s", e.Code, e.Status, e.Detail)
}

type Session struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Image string `json:"image"`
}
type Snapshot struct {
	ID               string            `json:"id"`
	SourceSessionID  string            `json:"source_session_id"`
	ParentSnapshotID string            `json:"parent_snapshot_id,omitempty"`
	SizeBytes        int64             `json:"size_bytes"`
	Flattened        bool              `json:"flattened,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	Labels           map[string]string `json:"labels"`
}
type FileInfo struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mode  uint32 `json:"mode"`
	IsDir bool   `json:"is_dir"`
}
type OutputChunk struct {
	Sequence  int64
	Cursor    int64
	Timestamp time.Time
	Stream    string
	Data      []byte
}
type ExecStreamResult struct {
	ID         string `json:"-"`
	State      string `json:"state"`
	ExitCode   *int   `json:"exit_code"`
	Signal     *int   `json:"signal"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
}
type ExecResult struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	ExitCode    *int   `json:"exit_code"`
	Signal      *int   `json:"signal"`
	OutputBytes int    `json:"output_bytes"`
	Truncated   bool   `json:"truncated"`
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	base := strings.TrimRight(c.BaseURL, "/")
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		var key [16]byte
		if _, err = rand.Read(key[:]); err != nil {
			return err
		}
		req.Header.Set("Idempotency-Key", hex.EncodeToString(key[:]))
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var p ProblemError
		_ = json.NewDecoder(res.Body).Decode(&p)
		p.Status = res.StatusCode
		return &p
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
func (c *Client) CreateSession(ctx context.Context, image string, limits map[string]int) (*Session, error) {
	body := map[string]any{"image": image}
	if len(limits) > 0 {
		body["limits"] = limits
	}
	var s Session
	err := c.request(ctx, http.MethodPost, "/v1/sessions", body, &s)
	return &s, err
}
func (c *Client) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := c.request(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(id), nil, &s)
	return &s, err
}
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.request(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(id), nil, nil)
}
func (c *Client) CreateSnapshot(ctx context.Context, id string, labels map[string]string) (*Snapshot, error) {
	body := map[string]any{}
	if len(labels) > 0 {
		body["labels"] = labels
	}
	var snapshot Snapshot
	err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(id)+"/snapshots", body, &snapshot)
	return &snapshot, err
}
func (c *Client) Fork(ctx context.Context, id, snapshotID string) (*Session, error) {
	body := map[string]string{}
	if snapshotID != "" {
		body["snapshot_id"] = snapshotID
	}
	var session Session
	err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(id)+"/fork", body, &session)
	return &session, err
}
func (c *Client) Exec(ctx context.Context, id string, argv []string, timeout time.Duration) (json.RawMessage, error) {
	body := map[string]any{"argv": argv, "timeout_ms": timeout.Milliseconds()}
	var r json.RawMessage
	err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(id)+"/execs?wait=true", body, &r)
	return r, err
}

// ExecStream starts a command and follows its persisted SSE output. If the
// connection ends early, use the returned ID and last OutputChunk.Cursor with
// ResumeExecStream to continue without starting another command.
func (c *Client) ExecStream(ctx context.Context, id string, argv []string, timeout time.Duration, onOutput func(OutputChunk) error) (ExecStreamResult, error) {
	var result ExecStreamResult
	body := map[string]any{"argv": argv, "timeout_ms": timeout.Milliseconds()}
	var started struct {
		ID        string `json:"id"`
		StreamURL string `json:"stream_url"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(id)+"/execs", body, &started); err != nil {
		return result, err
	}
	if started.ID == "" || started.StreamURL == "" {
		return result, fmt.Errorf("cellbox exec response did not include an id or stream_url")
	}
	return c.followExecStream(ctx, started.ID, started.StreamURL, 0, onOutput)
}

// ResumeExecStream follows an existing exec's output from the given SSE event
// cursor. Use the cursor supplied by the most recently received OutputChunk.
func (c *Client) ResumeExecStream(ctx context.Context, sessionID, execID string, after int64, onOutput func(OutputChunk) error) (ExecStreamResult, error) {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/execs/" + url.PathEscape(execID) + "/stream"
	return c.followExecStream(ctx, execID, path, after, onOutput)
}

func (c *Client) followExecStream(ctx context.Context, execID, streamURL string, after int64, onOutput func(OutputChunk) error) (ExecStreamResult, error) {
	result := ExecStreamResult{ID: execID}
	u, err := c.streamURL(streamURL)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if after > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(after, 10))
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return result, decodeProblem(res)
	}
	if !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
		return result, fmt.Errorf("cellbox exec stream returned content type %q", res.Header.Get("Content-Type"))
	}
	reader := bufio.NewReaderSize(res.Body, 32<<10)
	var eventName, eventData string
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && len(line) == 0 {
			if readErr == io.EOF {
				return result, io.ErrUnexpectedEOF
			}
			return result, readErr
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch {
		case line == "":
			if eventData != "" {
				done, err := c.handleExecEvent(eventName, eventData, &result, onOutput)
				if err != nil {
					return result, err
				}
				if done {
					return result, nil
				}
			}
			eventName, eventData = "", ""
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if eventData != "" {
				eventData += "\n"
			}
			eventData += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if readErr != nil {
			return result, io.ErrUnexpectedEOF
		}
	}
}

func (c *Client) streamURL(value string) (string, error) {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil {
		return "", err
	}
	stream, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	stream = base.ResolveReference(stream)
	if stream.Scheme != base.Scheme || !strings.EqualFold(stream.Host, base.Host) {
		return "", fmt.Errorf("cellbox stream URL is outside configured service")
	}
	return stream.String(), nil
}

func (c *Client) handleExecEvent(name, data string, result *ExecStreamResult, onOutput func(OutputChunk) error) (bool, error) {
	switch name {
	case "output":
		var event struct {
			Seq       int64     `json:"seq"`
			Timestamp time.Time `json:"ts"`
			Stream    string    `json:"stream"`
			Data      string    `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return false, err
		}
		decoded, err := base64.StdEncoding.DecodeString(event.Data)
		if err != nil {
			return false, fmt.Errorf("decode cellbox output event: %w", err)
		}
		if onOutput != nil {
			if err := onOutput(OutputChunk{Sequence: event.Seq, Cursor: event.Seq + int64(len(decoded)), Timestamp: event.Timestamp, Stream: event.Stream, Data: decoded}); err != nil {
				return false, err
			}
		}
	case "exit", "error":
		if err := json.Unmarshal([]byte(data), result); err != nil {
			return false, err
		}
		if name == "error" {
			return true, fmt.Errorf("cellbox exec stream ended in state %q", result.State)
		}
		return true, nil
	}
	return false, nil
}
func (c *Client) ReadFile(ctx context.Context, id, path string) ([]byte, error) {
	u := strings.TrimRight(c.BaseURL, "/") + "/v1/sessions/" + url.PathEscape(id) + "/files?path=" + url.QueryEscape(path)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, decodeProblem(res)
	}
	return io.ReadAll(res.Body)
}

func (c *Client) WriteFile(ctx context.Context, id, path string, contents []byte) error {
	u := strings.TrimRight(c.BaseURL, "/") + "/v1/sessions/" + url.PathEscape(id) + "/files?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(contents))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/octet-stream")
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return decodeProblem(res)
	}
	return nil
}

func (c *Client) ListFiles(ctx context.Context, id, path string, limit int) ([]FileInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if path == "" {
		path = "/"
	}
	var all []FileInfo
	cursor := ""
	for {
		query := url.Values{"path": {path}, "limit": {fmt.Sprint(limit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page struct {
			Items      []FileInfo `json:"items"`
			NextCursor string     `json:"next_cursor"`
		}
		endpoint := "/v1/sessions/" + url.PathEscape(id) + "/files/list?" + query.Encode()
		if err := c.request(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

func decodeProblem(res *http.Response) error {
	var p ProblemError
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&p)
	p.Status = res.StatusCode
	if p.Code == "" {
		p.Detail = res.Status
	}
	return &p
}
