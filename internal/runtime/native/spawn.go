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
	"sync"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

const initReadyTimeout = 5 * time.Second

// InitProcess is the host-side handle for one namespace init process.
type InitProcess struct {
	cmd     *exec.Cmd
	control *os.File
	stderr  *bytes.Buffer
	mu      sync.Mutex
	sendMu  sync.Mutex
	closed  bool
	request uint64
	active  string
}

// Exec launches one workload command inside the namespace, streams bounded output
// chunks through emit, and returns the final status.
func (p *InitProcess) Exec(ctx context.Context, request runtime.ExecRequest, emit func(runtime.OutputChunk) error, signals <-chan int) (runtime.ExecResult, error) {
	p.sendMu.Lock()
	p.mu.Lock()
	var empty runtime.ExecResult
	if p.closed || p.control == nil {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return empty, errors.New("native init process is closed")
	}
	readerFD, err := unix.Dup(int(p.control.Fd()))
	if err != nil {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return empty, fmt.Errorf("duplicate native control socket: %w", err)
	}
	unix.CloseOnExec(readerFD)
	reader := os.NewFile(uintptr(readerFD), "cellbox-init-reader")
	if p.active != "" {
		_ = reader.Close()
		p.mu.Unlock()
		p.sendMu.Unlock()
		return empty, errors.New("native init process already has an active exec")
	}
	if err := ctx.Err(); err != nil {
		_ = reader.Close()
		p.mu.Unlock()
		p.sendMu.Unlock()
		return empty, err
	}
	args := append([]string(nil), request.Argv...)
	if len(args) == 0 && request.Shell != "" {
		args = []string{"/bin/sh", "-lc", request.Shell}
	}
	payload, err := json.Marshal(workloadRequest{Argv: args, Cwd: request.Cwd, Env: request.Env, TimeoutMS: request.TimeoutMS, OutputLimit: request.OutputLimit})
	if err != nil {
		_ = reader.Close()
		p.mu.Unlock()
		p.sendMu.Unlock()
		return empty, fmt.Errorf("encode native workload request: %w", err)
	}
	p.request++
	id := fmt.Sprintf("exec-%d", p.request)
	p.active = id
	p.mu.Unlock()
	if err := SendControlMessage(int(p.control.Fd()), ControlMessage{Type: "exec", ID: id, Payload: payload}); err != nil {
		p.sendMu.Unlock()
		p.mu.Lock()
		p.active = ""
		p.mu.Unlock()
		return empty, fmt.Errorf("send native workload request: %w", err)
	}
	p.sendMu.Unlock()
	defer reader.Close()
	defer func() {
		p.mu.Lock()
		if p.active == id {
			p.active = ""
		}
		p.mu.Unlock()
	}()
	for {
		select {
		case number := <-signals:
			if err := p.sendExecSignal(id, number); err != nil {
				return empty, err
			}
		default:
		}
		if err := ctx.Err(); err != nil {
			p.sendMu.Lock()
			p.mu.Lock()
			p.closed = true
			control := p.control
			p.control = nil
			p.mu.Unlock()
			if control != nil {
				_ = control.Close()
			}
			p.sendMu.Unlock()
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
			return empty, err
		}
		fds := []unix.PollFd{{Fd: int32(reader.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		ready, pollErr := unix.Poll(fds, 100)
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil {
			return empty, fmt.Errorf("wait for native workload: %w", pollErr)
		}
		if ready == 0 {
			continue
		}
		message, files, recvErr := ReceiveControlMessage(int(reader.Fd()))
		closeFiles(files)
		if recvErr != nil {
			return empty, fmt.Errorf("read native workload result: %w", recvErr)
		}
		if message.ID != id {
			return empty, fmt.Errorf("native workload response id mismatch: got %q, want %q", message.ID, id)
		}
		if message.Type == "error" {
			return empty, errors.New(message.Error)
		}
		if message.Type == "exec_output" {
			if emit == nil {
				return empty, errors.New("native workload produced output without a receiver")
			}
			var chunk runtime.OutputChunk
			if err := json.Unmarshal(message.Payload, &chunk); err != nil {
				return empty, fmt.Errorf("decode native output chunk: %w", err)
			}
			if err := emit(chunk); err != nil {
				return empty, fmt.Errorf("deliver native output chunk: %w", err)
			}
			continue
		}
		if message.Type != "exec_done" {
			return empty, fmt.Errorf("unexpected native workload response %q", message.Type)
		}
		var result workloadResponse
		if err := json.Unmarshal(message.Payload, &result); err != nil {
			return empty, fmt.Errorf("decode native workload result: %w", err)
		}
		execResult := runtime.ExecResult{ExitCode: result.ExitCode, Signal: result.Signal, DurationMS: result.DurationMS, OutputBytes: result.OutputBytes, Truncated: result.Truncated}
		if result.TimedOut {
			return execResult, context.DeadlineExceeded
		}
		return execResult, nil
	}
}

func (p *InitProcess) sendExecSignal(id string, number int) error {
	if number < 1 || number > 64 {
		return fmt.Errorf("signal number %d is outside 1..64", number)
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.control == nil {
		return errors.New("native init process is closed")
	}
	if id == "" || p.active != id {
		return errors.New("native init process has no active exec")
	}
	payload, err := json.Marshal(struct {
		Signal int `json:"signal"`
	}{Signal: number})
	if err != nil {
		return err
	}
	return SendControlMessage(int(p.control.Fd()), ControlMessage{Type: "signal", ID: id, Payload: payload})
}

func (p *InitProcess) OpenTerminal(ctx context.Context, request runtime.TerminalRequest) (runtime.Terminal, error) {
	p.sendMu.Lock()
	p.mu.Lock()
	if p.closed || p.control == nil {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return nil, errors.New("native init process is closed")
	}
	if p.active != "" {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return nil, errors.New("native init process already has an active exec")
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return nil, err
	}
	readerFD, err := unix.Dup(int(p.control.Fd()))
	if err != nil {
		p.mu.Unlock()
		p.sendMu.Unlock()
		return nil, fmt.Errorf("duplicate native control socket: %w", err)
	}
	unix.CloseOnExec(readerFD)
	reader := os.NewFile(uintptr(readerFD), "cellbox-terminal-reader")
	p.request++
	id := fmt.Sprintf("terminal-%d", p.request)
	p.active = id
	payload, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh"}, Cwd: request.Cwd, Env: request.Env, TTY: true, Rows: request.Rows, Columns: request.Columns})
	if err != nil {
		p.active = ""
		p.mu.Unlock()
		p.sendMu.Unlock()
		_ = reader.Close()
		return nil, err
	}
	control := p.control
	p.mu.Unlock()
	if err := SendControlMessage(int(control.Fd()), ControlMessage{Type: "terminal", ID: id, Payload: payload}); err != nil {
		p.sendMu.Unlock()
		p.mu.Lock()
		p.active = ""
		p.mu.Unlock()
		_ = reader.Close()
		return nil, fmt.Errorf("send native terminal request: %w", err)
	}
	p.sendMu.Unlock()
	defer reader.Close()
	cancelled := false
	for {
		if err := ctx.Err(); err != nil && !cancelled {
			cancelled = true
			_ = p.sendExecSignal(id, int(syscall.SIGTERM))
		}
		fds := []unix.PollFd{{Fd: int32(reader.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		ready, pollErr := unix.Poll(fds, 100)
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil {
			p.releaseTerminal(id)
			return nil, fmt.Errorf("wait for native terminal: %w", pollErr)
		}
		if ready == 0 {
			continue
		}
		message, files, recvErr := ReceiveControlMessage(int(reader.Fd()))
		if recvErr != nil {
			p.releaseTerminal(id)
			return nil, fmt.Errorf("read native terminal response: %w", recvErr)
		}
		if message.ID != id {
			closeFiles(files)
			p.releaseTerminal(id)
			return nil, fmt.Errorf("native terminal response id mismatch: got %q, want %q", message.ID, id)
		}
		if message.Type == "error" {
			closeFiles(files)
			p.releaseTerminal(id)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, errors.New(message.Error)
		}
		if message.Type != "terminal_started" || len(files) != 1 {
			closeFiles(files)
			p.releaseTerminal(id)
			return nil, fmt.Errorf("unexpected native terminal response %q with %d descriptors", message.Type, len(files))
		}
		if err := ctx.Err(); err != nil {
			closeFiles(files)
			p.releaseTerminal(id)
			return nil, err
		}
		return newNativeTerminal(files[0], p, id), nil
	}
}

func (p *InitProcess) releaseTerminal(id string) {
	p.mu.Lock()
	if p.active == id {
		p.active = ""
	}
	p.mu.Unlock()
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

// Kill terminates init; Linux tears down the remaining PID namespace tasks with it.
func (p *InitProcess) Kill() error {
	if p == nil {
		return nil
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill native init: %w", err)
	}
	waitErr := p.cmd.Wait()
	p.closed = true
	if p.control != nil {
		_ = p.control.Close()
		p.control = nil
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return fmt.Errorf("reap native init: %w", waitErr)
		}
	}
	return nil
}

// Shutdown asks init to exit and reaps the child.
func (p *InitProcess) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.control == nil || p.closed {
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
		p.closed = true
		if err != nil {
			return fmt.Errorf("native init exited: %w (stderr: %s)", err, p.stderr.String())
		}
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		<-waitResult
		p.closed = true
		return ctx.Err()
	}
}
