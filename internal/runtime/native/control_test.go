//go:build linux

package native

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

func TestControlMessageTransfersJSONAndFileDescriptor(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	defer unix.Close(sockets[1])

	passed, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer passed.Close()
	want := ControlMessage{Type: "ready", ID: "ses_test", Payload: []byte(`{"pid":12}`)}
	if err := SendControlMessage(sockets[0], want, passed); err != nil {
		t.Fatal(err)
	}
	got, files, err := ReceiveControlMessage(sockets[1])
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles(files)
	if got.Type != want.Type || got.ID != want.ID || string(got.Payload) != string(want.Payload) {
		t.Fatalf("message = %+v, want %+v", got, want)
	}
	if len(files) != 1 {
		t.Fatalf("received %d descriptors, want 1", len(files))
	}
	if _, err := files[0].Stat(); err != nil {
		t.Fatalf("received descriptor is invalid: %v", err)
	}
}

func TestControlLoopSignalsRunningExec(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	defer unix.Close(sockets[1])
	loopDone := make(chan error, 1)
	go func() { loopDone <- controlLoop(sockets[0]) }()

	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sleep", "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := SendControlMessage(sockets[1], ControlMessage{Type: "exec", ID: "exec-test", Payload: request}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	signal, err := json.Marshal(struct {
		Signal int `json:"signal"`
	}{Signal: int(syscall.SIGTERM)})
	if err != nil {
		t.Fatal(err)
	}
	if err := SendControlMessage(sockets[1], ControlMessage{Type: "signal", ID: "exec-test", Payload: signal}); err != nil {
		t.Fatal(err)
	}
	message, files, err := ReceiveControlMessage(sockets[1])
	closeFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if message.Type != "exec_done" || message.ID != "exec-test" {
		t.Fatalf("control response = %+v", message)
	}
	var result workloadResponse
	if err := json.Unmarshal(message.Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Signal != int(syscall.SIGTERM) {
		t.Fatalf("exec result = %+v", result)
	}
	if err := SendControlMessage(sockets[1], ControlMessage{Type: "shutdown", ID: "shutdown-test"}); err != nil {
		t.Fatal(err)
	}
	message, files, err = ReceiveControlMessage(sockets[1])
	closeFiles(files)
	if err != nil || message.Type != "stopped" {
		t.Fatalf("shutdown response = %+v, err = %v", message, err)
	}
	if err := <-loopDone; err != nil {
		t.Fatalf("control loop: %v", err)
	}
}

func TestInitProcessOpenTerminalStreamsAndResizesPTY(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.SetNonblock(sockets[0], true)
	control := os.NewFile(uintptr(sockets[0]), "cellbox-test-control")
	defer control.Close()
	defer unix.Close(sockets[1])
	process := &InitProcess{control: control}
	loopDone := make(chan error, 1)
	go func() { loopDone <- controlLoop(sockets[1]) }()

	terminal, err := process.OpenTerminal(context.Background(), runtime.TerminalRequest{Columns: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Resize(context.Background(), 120, 40); err != nil {
		t.Fatalf("resize PTY: %v", err)
	}
	if _, err := terminal.Write([]byte("printf terminal-ok\nexit\n")); err != nil {
		t.Fatalf("write PTY: %v", err)
	}
	output, err := io.ReadAll(terminal)
	if err != nil {
		t.Fatalf("read PTY: %v", err)
	}
	if !strings.Contains(string(output), "terminal-ok") {
		t.Fatalf("PTY output = %q", output)
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY: %v", err)
	}
	if err := process.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown init process: %v", err)
	}
	if err := <-loopDone; err != nil {
		t.Fatalf("control loop: %v", err)
	}
}

func TestSendControlMessageRejectsOversizeAndNilFile(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	defer unix.Close(sockets[1])
	if err := SendControlMessage(sockets[0], ControlMessage{Type: "data", Payload: make([]byte, maxControlMessage)}); err == nil {
		t.Fatal("accepted oversized control packet")
	}
	if err := SendControlMessage(sockets[0], ControlMessage{Type: "fd"}, nil); err == nil {
		t.Fatal("accepted nil file descriptor")
	}
}

func TestInitProcessRunsConcurrentExecsAndTTYExec(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.SetNonblock(sockets[0], true)
	control := os.NewFile(uintptr(sockets[0]), "cellbox-test-control")
	defer control.Close()
	defer unix.Close(sockets[1])
	process := &InitProcess{control: control}
	loopDone := make(chan error, 1)
	go func() { loopDone <- controlLoop(sockets[1]) }()

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var out bytes.Buffer
			res, err := process.Exec(context.Background(), runtime.ExecRequest{
				Argv:      []string{"/bin/sh", "-c", "sleep 0.05; printf concurrent"},
				TimeoutMS: 5000,
			}, func(chunk runtime.OutputChunk) error {
				out.Write(chunk.Data)
				return nil
			}, nil)
			if err != nil || res.ExitCode != 0 || out.String() != "concurrent" {
				errs <- errors.New("concurrent exec mismatch")
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ttyOut bytes.Buffer
		var ttyStream string
		res, err := process.Exec(context.Background(), runtime.ExecRequest{
			Argv:      []string{"/bin/sh", "-c", "printf tty-exec-ok"},
			TTY:       true,
			TimeoutMS: 5000,
		}, func(chunk runtime.OutputChunk) error {
			ttyStream = chunk.Stream
			ttyOut.Write(chunk.Data)
			return nil
		}, nil)
		if err != nil || res.ExitCode != 0 || ttyStream != "tty" || !strings.Contains(ttyOut.String(), "tty-exec-ok") {
			errs <- errors.New("tty exec mismatch")
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if err := process.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown init process: %v", err)
	}
	if err := <-loopDone; err != nil {
		t.Fatalf("control loop: %v", err)
	}
}

func TestInitProcessMediatesFileAndArchiveOperations(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.SetNonblock(sockets[0], true)
	control := os.NewFile(uintptr(sockets[0]), "cellbox-test-control")
	defer control.Close()
	_ = unix.SetNonblock(sockets[1], true)
	peer := os.NewFile(uintptr(sockets[1]), "cellbox-test-peer")
	defer peer.Close()
	loopDone := make(chan error, 1)
	go func() { loopDone <- controlLoopFile(peer, root) }()

	process := &InitProcess{control: control}
	box := &sandbox{init: process}
	ctx := context.Background()

	if err := box.WriteFile(ctx, "/work/hello.txt", strings.NewReader("init-mediated"), 0640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := box.WriteFile(ctx, "/work/hello.txt", errorAfterBytes{}, 0640); !errors.Is(err, errTestRead) {
		t.Fatalf("WriteFile abort error = %v", err)
	}
	rc, info, err := box.ReadFile(ctx, "/work/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data, readErr := io.ReadAll(rc)
	_ = rc.Close()
	if readErr != nil || string(data) != "init-mediated" || info.Size != int64(len("init-mediated")) {
		t.Fatalf("ReadFile data=%q info=%+v err=%v", data, info, readErr)
	}
	entries, err := box.ListDir(ctx, "/work")
	if err != nil || len(entries) != 1 || entries[0].Path != "/work/hello.txt" {
		t.Fatalf("ListDir entries=%+v err=%v", entries, err)
	}
	archived, err := box.Archive(ctx, "/work")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	archiveBytes, err := io.ReadAll(archived)
	_ = archived.Close()
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "extracted"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := box.Extract(ctx, "/extracted", bytes.NewReader(archiveBytes)); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	gotExtracted, err := os.ReadFile(filepath.Join(root, "extracted", "hello.txt"))
	if err != nil || string(gotExtracted) != "init-mediated" {
		t.Fatalf("extracted file=%q err=%v", gotExtracted, err)
	}
	var badTar bytes.Buffer
	tw := tar.NewWriter(&badTar)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	if err := box.Extract(ctx, "/extracted", bytes.NewReader(badTar.Bytes())); err == nil {
		t.Fatal("Extract accepted traversal archive")
	}
	if err := box.DeleteFile(ctx, "/work/hello.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, _, err := box.ReadFile(ctx, "/work/hello.txt"); err == nil {
		t.Fatal("ReadFile succeeded after DeleteFile")
	}
	if err := process.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown init process: %v", err)
	}
	if err := <-loopDone; err != nil {
		t.Fatalf("control loop: %v", err)
	}
}
