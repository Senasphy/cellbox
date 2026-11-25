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
	data, rights, err := encodeControlMessage(message, files...)
	if err != nil {
		return err
	}
	return sendControlPacket(socketFD, data, rights)
}

func sendControlMessageFile(socket *os.File, message ControlMessage, files ...*os.File) error {
	data, rights, err := encodeControlMessage(message, files...)
	if err != nil {
		return err
	}
	conn, err := socket.SyscallConn()
	if err != nil {
		return sendControlPacket(int(socket.Fd()), data, rights)
	}
	var sendErr error
	if ctrlErr := conn.Write(func(fd uintptr) bool {
		sendErr = sendControlPacket(int(fd), data, rights)
		return !errors.Is(sendErr, unix.EAGAIN) && !errors.Is(sendErr, unix.EWOULDBLOCK)
	}); ctrlErr != nil {
		return ctrlErr
	}
	return sendErr
}

func encodeControlMessage(message ControlMessage, files ...*os.File) ([]byte, []byte, error) {
	if len(files) > maxControlFiles {
		return nil, nil, fmt.Errorf("too many control file descriptors: %d", len(files))
	}
	data, err := json.Marshal(message)
	if err != nil {
		return nil, nil, fmt.Errorf("encode control message: %w", err)
	}
	if len(data) > maxControlMessage {
		return nil, nil, fmt.Errorf("control message exceeds %d bytes", maxControlMessage)
	}
	var rights []byte
	if len(files) > 0 {
		fds := make([]int, len(files))
		for i, file := range files {
			if file == nil {
				return nil, nil, fmt.Errorf("control file descriptor %d is nil", i)
			}
			fds[i] = int(file.Fd())
		}
		rights = unix.UnixRights(fds...)
	}
	return data, rights, nil
}

func sendControlPacket(socketFD int, data, rights []byte) error {
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
	data := make([]byte, maxControlMessage)
	oob := make([]byte, unix.CmsgSpace(maxControlFiles*4))
	n, oobn, flags, _, err := unix.Recvmsg(socketFD, data, oob, 0)
	if err != nil {
		return ControlMessage{}, nil, fmt.Errorf("receive control message: %w", err)
	}
	return decodeControlPacket(data[:n], oob[:oobn], flags)
}

func receiveControlMessageFile(socket *os.File) (ControlMessage, []*os.File, error) {
	conn, err := socket.SyscallConn()
	if err != nil {
		return ReceiveControlMessage(int(socket.Fd()))
	}
	data := make([]byte, maxControlMessage)
	oob := make([]byte, unix.CmsgSpace(maxControlFiles*4))
	var (
		n, oobn, flags int
		recvErr        error
	)
	if ctrlErr := conn.Read(func(fd uintptr) bool {
		n, oobn, flags, _, recvErr = unix.Recvmsg(int(fd), data, oob, 0)
		return !errors.Is(recvErr, unix.EAGAIN) && !errors.Is(recvErr, unix.EWOULDBLOCK)
	}); ctrlErr != nil {
		return ControlMessage{}, nil, ctrlErr
	}
	if recvErr != nil {
		return ControlMessage{}, nil, fmt.Errorf("receive control message: %w", recvErr)
	}
	return decodeControlPacket(data[:n], oob[:oobn], flags)
}

func decodeControlPacket(data, oob []byte, flags int) (ControlMessage, []*os.File, error) {
	var message ControlMessage
	controlMessages, err := unix.ParseSocketControlMessage(oob)
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
	if err := json.Unmarshal(data, &message); err != nil {
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
