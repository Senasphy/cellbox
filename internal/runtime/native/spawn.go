//go:build linux

package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const initReadyTimeout = 5 * time.Second

// InitProcess is the host-side handle for one namespace init process.
type InitProcess struct {
	cmd     *exec.Cmd
	control *os.File
	stderr  *bytes.Buffer
}

// SpawnInit re-execs this binary into the requested namespaces and waits for
// the init process to complete mount setup before returning.
func SpawnInit(ctx context.Context, config InitConfig, cgroupFD int) (*InitProcess, error) {
	if err := validateInitConfig(config); err != nil {
		return nil, err
	}
	if cgroupFD < 0 {
		return nil, errors.New("native init requires a cgroup directory fd")
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("create init control socketpair: %w", err)
	}
	parentSocket := os.NewFile(uintptr(pair[0]), "cellbox-init-parent")
	childSocket := os.NewFile(uintptr(pair[1]), "cellbox-init-child")
	configRead, configWrite, err := os.Pipe()
	if err != nil {
		parentSocket.Close()
		childSocket.Close()
		return nil, fmt.Errorf("create init config pipe: %w", err)
	}
	command := exec.Command("/proc/self/exe")
	command.Args = []string{"cellbox-init"}
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/"}
	command.ExtraFiles = []*os.File{childSocket, configRead}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS | syscall.CLONE_NEWCGROUP,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
		UseCgroupFD:                true,
		CgroupFD:                   cgroupFD,
		Pdeathsig:                  syscall.SIGKILL,
		Setsid:                     true,
	}
	if err := command.Start(); err != nil {
		parentSocket.Close()
		childSocket.Close()
		configRead.Close()
		configWrite.Close()
		return nil, fmt.Errorf("start native namespace init: %w", err)
	}
	childSocket.Close()
	configRead.Close()
	encoded, err := json.Marshal(config)
	if err == nil {
		_, err = configWrite.Write(encoded)
	}
	configWrite.Close()
	if err != nil {
		return nil, failStartedInit(command, parentSocket, fmt.Errorf("write init config: %w", err), stderr)
	}
	deadline := time.Now().Add(initReadyTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, failStartedInit(command, parentSocket, err, stderr)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, failStartedInit(command, parentSocket, errors.New("timed out waiting for native init ready"), stderr)
		}
		pollMS := int(remaining.Milliseconds())
		if pollMS < 1 {
			pollMS = 1
		}
		fds := []unix.PollFd{{Fd: int32(parentSocket.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		n, pollErr := unix.Poll(fds, pollMS)
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil {
			return nil, failStartedInit(command, parentSocket, fmt.Errorf("wait for native init: %w", pollErr), stderr)
		}
		if n == 0 {
			continue
		}
		message, files, recvErr := ReceiveControlMessage(int(parentSocket.Fd()))
		closeFiles(files)
		if recvErr != nil {
			return nil, failStartedInit(command, parentSocket, fmt.Errorf("read native init ready: %w", recvErr), stderr)
		}
		if message.Type != "ready" || message.ID != config.SessionID {
			msgErr := fmt.Errorf("native init failed before ready: %s", message.Error)
			return nil, failStartedInit(command, parentSocket, msgErr, stderr)
		}
		return &InitProcess{cmd: command, control: parentSocket, stderr: stderr}, nil
	}
}

func failStartedInit(command *exec.Cmd, control *os.File, cause error, stderr *bytes.Buffer) error {
	_ = control.Close()
	if command.Process != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
	}
	if stderr != nil && stderr.Len() > 0 {
		return fmt.Errorf("%w (init stderr: %s)", cause, stderr.String())
	}
	return cause
}

// Shutdown asks init to exit and reaps the child.
func (p *InitProcess) Shutdown(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.control == nil {
		return nil
	}
	defer p.control.Close()
	if err := SendControlMessage(int(p.control.Fd()), ControlMessage{Type: "shutdown"}); err != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		return fmt.Errorf("send native init shutdown: %w", err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- p.cmd.Wait() }()
	select {
	case err := <-waitResult:
		if err != nil {
			return fmt.Errorf("native init exited: %w (stderr: %s)", err, p.stderr.String())
		}
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		<-waitResult
		return ctx.Err()
	}
}
