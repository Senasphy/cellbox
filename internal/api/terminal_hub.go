package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

const (
	terminalScrollbackSize = 256 << 10
	terminalReattachGrace  = 30 * time.Second
)

type terminalHub struct {
	mu      sync.Mutex
	term    runtime.Terminal
	grace   time.Duration
	ring    []byte
	client  *terminalClient
	timer   *time.Timer
	ended   bool
	onClose func()
}

type terminalClient struct {
	output chan []byte
	done   chan struct{}
}

func (s *server) terminalForSession(id string, box runtime.Sandbox, ctx context.Context, env map[string]string) (*terminalHub, error) {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if hub := s.terminals[id]; hub != nil {
		return hub, nil
	}
	term, err := box.OpenTerminal(ctx, runtime.TerminalRequest{Columns: 80, Rows: 24, Env: env})
	if err != nil {
		return nil, err
	}
	grace := time.Duration(s.cfg.Terminal.GraceS) * time.Second
	if grace <= 0 {
		grace = terminalReattachGrace
	}
	hub := &terminalHub{term: term, grace: grace, ring: make([]byte, 0, terminalScrollbackSize)}
	hub.onClose = func() {
		s.terminalMu.Lock()
		if s.terminals[id] == hub {
			delete(s.terminals, id)
		}
		s.terminalMu.Unlock()
	}
	s.terminals[id] = hub
	go hub.readLoop()
	return hub, nil
}

func (h *terminalHub) attach() (*terminalClient, []byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ended {
		return nil, nil, errors.New("terminal has ended")
	}
	if h.client != nil {
		return nil, nil, errors.New("terminal already has an attached client")
	}
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	client := &terminalClient{output: make(chan []byte, 64), done: make(chan struct{})}
	h.client = client
	return client, append([]byte(nil), h.ring...), nil
}

func (h *terminalHub) detach(client *terminalClient) {
	h.mu.Lock()
	if h.client != client {
		h.mu.Unlock()
		return
	}
	h.client = nil
	close(client.done)
	if !h.ended {
		grace := h.grace
		if grace <= 0 {
			grace = terminalReattachGrace
		}
		h.timer = time.AfterFunc(grace, func() { h.finish(false) })
	}
	h.mu.Unlock()
}

func (h *terminalHub) write(client *terminalClient, data []byte) (int, error) {
	h.mu.Lock()
	if h.ended || h.client != client {
		h.mu.Unlock()
		return 0, errors.New("terminal client is detached")
	}
	term := h.term
	h.mu.Unlock()
	return term.Write(data)
}

func (h *terminalHub) resize(ctx context.Context, client *terminalClient, columns, rows uint16) error {
	h.mu.Lock()
	if h.ended || h.client != client {
		h.mu.Unlock()
		return errors.New("terminal client is detached")
	}
	term := h.term
	h.mu.Unlock()
	return term.Resize(ctx, columns, rows)
}

func (h *terminalHub) readLoop() {
	buffer := make([]byte, 16<<10)
	for {
		n, err := h.term.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			h.mu.Lock()
			h.ring = appendTerminalRing(h.ring, chunk)
			client := h.client
			h.mu.Unlock()
			if client != nil {
				select {
				case client.output <- chunk:
				case <-client.done:
				}
			}
		}
		if err != nil {
			h.finish(true)
			return
		}
	}
}

func appendTerminalRing(ring, chunk []byte) []byte {
	if len(chunk) >= terminalScrollbackSize {
		return append([]byte(nil), chunk[len(chunk)-terminalScrollbackSize:]...)
	}
	if overflow := len(ring) + len(chunk) - terminalScrollbackSize; overflow > 0 {
		copy(ring, ring[overflow:])
		ring = ring[:len(ring)-overflow]
	}
	return append(ring, chunk...)
}

func (h *terminalHub) finish(force bool) {
	h.mu.Lock()
	if h.ended || (!force && h.client != nil) {
		h.mu.Unlock()
		return
	}
	h.ended = true
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	if h.client != nil {
		close(h.client.done)
		h.client = nil
	}
	term, onClose := h.term, h.onClose
	h.mu.Unlock()
	_ = term.Close()
	if onClose != nil {
		onClose()
	}
}

func (s *server) closeTerminal(id string) {
	s.terminalMu.Lock()
	hub := s.terminals[id]
	s.terminalMu.Unlock()
	if hub != nil {
		hub.finish(true)
	}
}
