package egress

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestForwarderBridgesLoopbackToUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	upstream, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	forwarder, err := NewForwarder("127.0.0.1:0", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- forwarder.Serve(ctx) }()

	client, err := net.DialTimeout("tcp", forwarder.Addr().String(), time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprint(client, "GET http://example.test/ HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
		cancel()
		t.Fatal(err)
	}
	conn, err := upstream.AcceptUnix()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	if line, err := bufio.NewReader(conn).ReadString('\n'); err != nil || line != "GET http://example.test/ HTTP/1.1\r\n" {
		cancel()
		t.Fatalf("forwarded request line = %q, err=%v", line, err)
	}
	if _, err := fmt.Fprint(conn, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"); err != nil {
		cancel()
		t.Fatal(err)
	}
	line, err := bufio.NewReader(client).ReadString('\n')
	if err != nil || line != "HTTP/1.1 204 No Content\r\n" {
		cancel()
		t.Fatalf("response line = %q, err=%v", line, err)
	}

	cancel()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestNewForwarderRequiresSocket(t *testing.T) {
	if _, err := NewForwarder("127.0.0.1:0", ""); err == nil {
		t.Fatal("NewForwarder accepted an empty Unix socket path")
	}
}
