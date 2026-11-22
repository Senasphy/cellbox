//go:build linux

package native

import (
	"io"
	"net"
	"time"
)

func startEgressForwarder() error {
	listener, err := net.Listen("tcp", "127.0.0.1:3128")
	if err != nil {
		return err
	}
	slots := make(chan struct{}, 64)
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				go func() {
					defer func() { <-slots }()
					forwardEgressConnection(client)
				}()
			default:
				_ = client.Close()
			}
		}
	}()
	return nil
}

func forwardEgressConnection(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Minute))
	host, err := net.DialTimeout("unix", "/run/cellbox/egress.sock", 5*time.Second)
	if err != nil {
		return
	}
	defer host.Close()
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
