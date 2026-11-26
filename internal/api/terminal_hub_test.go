package api

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

type hubTestTerminal struct {
	output chan []byte
	done   chan struct{}
	once   sync.Once
}

func newHubTestTerminal() *hubTestTerminal {
	return &hubTestTerminal{output: make(chan []byte, 8), done: make(chan struct{})}
}

func (t *hubTestTerminal) Read(buffer []byte) (int, error) {
	select {
	case data := <-t.output:
		return copy(buffer, data), nil
	case <-t.done:
		return 0, io.EOF
	}
}

func (*hubTestTerminal) Write(data []byte) (int, error)               { return len(data), nil }
func (*hubTestTerminal) Resize(context.Context, uint16, uint16) error { return nil }
func (t *hubTestTerminal) Close() error {
	t.once.Do(func() { close(t.done) })
	return nil
}

func TestTerminalHubReattachesWithBoundedScrollback(t *testing.T) {
	term := newHubTestTerminal()
	hub := &terminalHub{term: term, grace: 5 * time.Second, ring: make([]byte, 0, terminalScrollbackSize)}
	go hub.readLoop()
	first, _, err := hub.attach()
	if err != nil {
		t.Fatal(err)
	}
	term.output <- []byte("before disconnect")
	select {
	case got := <-first.output:
		if string(got) != "before disconnect" {
			t.Fatalf("first output=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal output was not forwarded")
	}
	hub.detach(first)
	term.output <- []byte(" while detached")
	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.Lock()
		got := string(hub.ring)
		hub.mu.Unlock()
		if got == "before disconnect while detached" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scrollback=%q", got)
		}
		time.Sleep(time.Millisecond)
	}
	second, scrollback, err := hub.attach()
	if err != nil {
		t.Fatal(err)
	}
	if string(scrollback) != "before disconnect while detached" {
		t.Fatalf("reattach scrollback=%q", scrollback)
	}
	if _, err := hub.write(second, []byte("input")); err != nil {
		t.Fatalf("reattached input: %v", err)
	}
	hub.finish(true)
	select {
	case <-second.done:
	case <-time.After(time.Second):
		t.Fatal("closing the terminal did not detach the client")
	}
	var _ runtime.Terminal = term
}

func TestAppendTerminalRingKeepsNewestBytes(t *testing.T) {
	ring := make([]byte, terminalScrollbackSize)
	for i := range ring {
		ring[i] = 'x'
	}
	ring = appendTerminalRing(ring, []byte("tail"))
	if len(ring) != terminalScrollbackSize || string(ring[len(ring)-4:]) != "tail" {
		t.Fatalf("ring length=%d tail=%q", len(ring), ring[len(ring)-4:])
	}
}

func TestTerminalHubExpiresAfterConfiguredGracePeriod(t *testing.T) {
	term := newHubTestTerminal()
	closed := make(chan struct{})
	hub := &terminalHub{term: term, grace: 25 * time.Millisecond, ring: make([]byte, 0, terminalScrollbackSize), onClose: func() { close(closed) }}
	go hub.readLoop()
	client, _, err := hub.attach()
	if err != nil {
		t.Fatal(err)
	}
	hub.detach(client)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("expected terminal hub to close after configured grace period")
	}
	if _, _, err := hub.attach(); err == nil {
		t.Fatal("expected reattach after grace expiry to fail")
	}
}
