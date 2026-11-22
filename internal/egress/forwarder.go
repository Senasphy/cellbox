package egress

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Forwarder tunnels TCP connections on a sandbox loopback address to a host
// egress proxy over a mounted Unix socket.
type Forwarder struct {
	listener net.Listener
	socket   string
	slots    chan struct{}
	mu       sync.Mutex
	active   map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
	close    sync.Once
}

// NewForwarder binds the listener before returning so callers can fail session
// startup if the proxy address is unavailable.
func NewForwarder(address, socket string) (*Forwarder, error) {
	if socket == "" {
		return nil, errors.New("egress Unix socket path is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &Forwarder{listener: listener, socket: socket, slots: make(chan struct{}, 64), active: make(map[net.Conn]struct{})}, nil
}

// Addr returns the TCP address bound by NewForwarder.
func (f *Forwarder) Addr() net.Addr { return f.listener.Addr() }

// Serve accepts proxy connections until ctx is canceled or the listener fails.
func (f *Forwarder) Serve(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = f.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer f.Close()
	for {
		client, err := f.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case f.slots <- struct{}{}:
			if !f.track(client) {
				<-f.slots
				_ = client.Close()
				continue
			}
			go func() {
				defer f.wg.Done()
				defer func() { <-f.slots }()
				f.forward(ctx, client)
			}()
		default:
			_ = client.Close()
		}
	}
}

// Close stops accepting connections and closes all active tunnels.
func (f *Forwarder) Close() error {
	f.close.Do(func() {
		f.mu.Lock()
		f.closed = true
		for conn := range f.active {
			_ = conn.Close()
			delete(f.active, conn)
		}
		f.mu.Unlock()
		_ = f.listener.Close()
	})
	f.wg.Wait()
	return nil
}

func (f *Forwarder) track(conn net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.active[conn] = struct{}{}
	f.wg.Add(1)
	return true
}

func (f *Forwarder) trackHost(conn net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	f.active[conn] = struct{}{}
	return true
}

func (f *Forwarder) untrack(conn net.Conn) {
	f.mu.Lock()
	delete(f.active, conn)
	f.mu.Unlock()
}

func (f *Forwarder) forward(ctx context.Context, client net.Conn) {
	defer f.untrack(client)
	_ = client.SetDeadline(time.Now().Add(2 * time.Minute))
	host, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", f.socket)
	if err != nil {
		_ = client.Close()
		return
	}
	if !f.trackHost(host) {
		_ = host.Close()
		_ = client.Close()
		return
	}
	defer f.untrack(host)
	defer host.Close()
	defer client.Close()
	_ = host.SetDeadline(time.Now().Add(2 * time.Minute))
	finished := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(host, client)
		if unix, ok := host.(*net.UnixConn); ok {
			_ = unix.CloseWrite()
		}
		finished <- struct{}{}
	}()
	_, _ = io.Copy(client, host)
	_ = client.SetDeadline(time.Now())
	<-finished
}
