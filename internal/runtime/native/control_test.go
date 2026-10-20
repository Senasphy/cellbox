//go:build linux

package native

import (
	"os"
	"testing"

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
