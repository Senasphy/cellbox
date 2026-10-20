package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func newShellCommand() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{Use: "shell SESSION_ID", Short: "Attach an interactive shell to a session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		return attachShell(cmd.Context(), client, base, key, args[0], os.Stdin, os.Stdout, os.Stderr)
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	return cmd
}

func attachShell(ctx context.Context, client *http.Client, base, key, id string, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.Contains(id, "/") || id == "" {
		return fmt.Errorf("invalid session id")
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return fmt.Errorf("cellbox URL must use http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/sessions/" + url.PathEscape(id) + "/terminal"
	header := http.Header{"Authorization": []string{"Bearer " + key}}
	dialer := websocket.Dialer{HandshakeTimeout: client.Timeout}
	conn, response, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect terminal: %s", response.Status)
		}
		return fmt.Errorf("connect terminal: %w", err)
	}
	defer conn.Close()
	var saved *unix.Termios
	if file, ok := stdin.(*os.File); ok {
		if state, e := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS); e == nil {
			saved = state
			raw := *state
			raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
			raw.Oflag &^= unix.OPOST
			raw.Cflag |= unix.CS8
			raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
			raw.Cc[unix.VMIN] = 1
			raw.Cc[unix.VTIME] = 0
			if e = unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &raw); e != nil {
				return fmt.Errorf("set terminal raw mode: %w", e)
			}
			defer func() { _ = unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, saved) }()
		}
	}
	writeMu := sync.Mutex{}
	write := func(kind int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(kind, payload)
	}
	resize := func() error {
		file, ok := stdin.(*os.File)
		if !ok {
			return nil
		}
		size, e := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
		if e != nil || size.Col == 0 || size.Row == 0 {
			return nil
		}
		data, _ := json.Marshal(map[string]any{"type": "resize", "columns": size.Col, "rows": size.Row})
		return write(websocket.TextMessage, data)
	}
	if err := resize(); err != nil {
		return err
	}
	var stopResize func()
	if _, ok := stdin.(*os.File); ok {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGWINCH)
		resizeCtx, cancel := context.WithCancel(ctx)
		stopResize = cancel
		go func() {
			for {
				select {
				case <-resizeCtx.Done():
					return
				case <-signals:
					if err := resize(); err != nil {
						return
					}
				}
			}
		}()
		defer func() { stopResize(); signal.Stop(signals) }()
	}
	inputDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := stdin.Read(buf)
			if n > 0 {
				if writeErr := write(websocket.BinaryMessage, append([]byte(nil), buf[:n]...)); writeErr != nil {
					inputDone <- writeErr
					return
				}
			}
			if e != nil {
				if errors.Is(e, io.EOF) {
					inputDone <- io.EOF
				} else {
					inputDone <- e
				}
				return
			}
		}
	}()
	outputDone := make(chan error, 1)
	go func() {
		for {
			kind, payload, e := conn.ReadMessage()
			if e != nil {
				outputDone <- e
				return
			}
			if kind == websocket.BinaryMessage {
				if _, e = stdout.Write(payload); e != nil {
					outputDone <- e
					return
				}
			} else if kind == websocket.TextMessage {
				var notice struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				}
				if json.Unmarshal(payload, &notice) == nil && notice.Type == "error" {
					outputDone <- fmt.Errorf("terminal: %s", notice.Message)
					return
				}
			}
		}
	}()
	for {
		select {
		case err := <-outputDone:
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case err := <-inputDone:
			if errors.Is(err, io.EOF) {
				_ = write(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "stdin closed"))
				inputDone = nil
				continue
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
