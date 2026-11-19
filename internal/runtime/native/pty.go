//go:build linux

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

type nativeTerminal struct {
	file  *os.File
	owner *InitProcess
	id    string
	once  sync.Once
	err   error
}

func newNativeTerminal(file *os.File, owner *InitProcess, id string) *nativeTerminal {
	return &nativeTerminal{file: file, owner: owner, id: id}
}

func (t *nativeTerminal) Read(buffer []byte) (int, error) {
	n, err := t.file.Read(buffer)
	if errors.Is(err, unix.EIO) {
		t.finish()
		return n, io.EOF
	}
	if err == io.EOF {
		t.finish()
	}
	return n, err
}

func (t *nativeTerminal) Write(buffer []byte) (int, error) { return t.file.Write(buffer) }

func (t *nativeTerminal) Resize(ctx context.Context, columns, rows uint16) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return unix.IoctlSetWinsize(int(t.file.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: columns})
}

func (t *nativeTerminal) Close() error {
	t.finish()
	return t.err
}

func (t *nativeTerminal) finish() {
	t.once.Do(func() {
		t.err = t.file.Close()
		t.owner.releaseTerminal(t.id)
	})
}

func openNativePTY(rows, columns uint16) (*os.File, *os.File, error) {
	if rows == 0 {
		rows = 24
	}
	if columns == 0 {
		columns = 80
	}
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open PTY master: %w", err)
	}
	closeMaster := func() { _ = unix.Close(masterFD) }
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		closeMaster()
		return nil, nil, fmt.Errorf("unlock PTY slave: %w", err)
	}
	index, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		closeMaster()
		return nil, nil, fmt.Errorf("get PTY slave index: %w", err)
	}
	slaveFD, err := unix.Open(fmt.Sprintf("/dev/pts/%d", index), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		closeMaster()
		return nil, nil, fmt.Errorf("open PTY slave: %w", err)
	}
	if err := unix.IoctlSetWinsize(masterFD, unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: columns}); err != nil {
		unix.Close(slaveFD)
		closeMaster()
		return nil, nil, fmt.Errorf("set PTY size: %w", err)
	}
	return os.NewFile(uintptr(masterFD), "cellbox-pty-master"), os.NewFile(uintptr(slaveFD), "cellbox-pty-slave"), nil
}
