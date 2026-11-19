//go:build linux

package native

import (
	"encoding/json"
	"os"
	"syscall"
	"testing"
	"time"

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
