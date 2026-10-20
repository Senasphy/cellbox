//go:build linux

package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	maxControlMessage = 1 << 20
	maxControlFiles   = 16
)

// ControlMessage is one framed request or response on the native SOCK_SEQPACKET channel.
type ControlMessage struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// SendControlMessage sends one JSON packet and optional file descriptors.
func SendControlMessage(socketFD int, message ControlMessage, files ...*os.File) error {
	if len(files) > maxControlFiles {
		return fmt.Errorf("too many control file descriptors: %d", len(files))
	}
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode control message: %w", err)
	}
	if len(data) > maxControlMessage {
		return fmt.Errorf("control message exceeds %d bytes", maxControlMessage)
	}
	var rights []byte
	if len(files) > 0 {
		fds := make([]int, len(files))
		for i, file := range files {
			if file == nil {
				return fmt.Errorf("control file descriptor %d is nil", i)
			}
			fds[i] = int(file.Fd())
		}
		rights = unix.UnixRights(fds...)
	}
	n, err := unix.SendmsgN(socketFD, data, rights, nil, 0)
	if err != nil {
		return fmt.Errorf("send control message: %w", err)
	}
	if n != len(data) {
		return errors.New("short SOCK_SEQPACKET control message write")
	}
	return nil
}

// ReceiveControlMessage receives one JSON packet and takes ownership of received descriptors.
func ReceiveControlMessage(socketFD int) (ControlMessage, []*os.File, error) {
	var message ControlMessage
	data := make([]byte, maxControlMessage)
	oob := make([]byte, unix.CmsgSpace(maxControlFiles*4))
	n, oobn, flags, _, err := unix.Recvmsg(socketFD, data, oob, 0)
	if err != nil {
		return message, nil, fmt.Errorf("receive control message: %w", err)
	}
	controlMessages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return message, nil, fmt.Errorf("parse control ancillary data: %w", err)
	}
	var files []*os.File
	for _, control := range controlMessages {
		fds, err := unix.ParseUnixRights(&control)
		if err != nil {
			closeFiles(files)
			return message, nil, fmt.Errorf("parse passed file descriptors: %w", err)
		}
		for _, fd := range fds {
			unix.CloseOnExec(fd)
			files = append(files, os.NewFile(uintptr(fd), "cellbox-control-fd"))
		}
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		closeFiles(files)
		return message, nil, errors.New("truncated native control packet")
	}
	if len(files) > maxControlFiles {
		closeFiles(files)
		return message, nil, fmt.Errorf("received too many control file descriptors: %d", len(files))
	}
	if err := json.Unmarshal(data[:n], &message); err != nil {
		closeFiles(files)
		return message, nil, fmt.Errorf("decode control message: %w", err)
	}
	return message, files, nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
