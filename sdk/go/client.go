// Package cellbox is a small typed client for the cellbox HTTP API.
package cellbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
func (c *Client) Exec(ctx context.Context, id string, argv []string, timeout time.Duration) (json.RawMessage, error) {
	body := map[string]any{"argv": argv, "timeout_ms": timeout.Milliseconds()}
	var r json.RawMessage
	err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(id)+"/execs?wait=true", body, &r)
	return r, err
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
		return nil, fmt.Errorf("cellbox file read: %s", res.Status)
	}
	return io.ReadAll(res.Body)
}
