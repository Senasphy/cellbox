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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

const initReadyTimeout = 5 * time.Second

type controlPacket struct {
	msg   ControlMessage
	files []*os.File
}

type pendingRequest struct {
	ch   chan controlPacket
	done chan struct{}
}

// InitProcess is the host-side handle for one namespace init process.
type InitProcess struct {
	cmd        *exec.Cmd
	control    *os.File
	stderr     *bytes.Buffer
	mu         sync.Mutex
	sendMu     sync.Mutex
	closed     bool
	request    uint64
	pending    map[string]*pendingRequest
	readerOnce sync.Once
	readerDone chan struct{}
	readerErr  error
	waitOnce   sync.Once
	waitErr    error
}

func (p *InitProcess) waitCmd() error {
	p.waitOnce.Do(func() {
		if p.cmd != nil {
			p.waitErr = p.cmd.Wait()
		}
	})
	return p.waitErr
}

func (p *InitProcess) ensureReader() {
	p.readerOnce.Do(func() {
		p.mu.Lock()
		if p.pending == nil {
			p.pending = make(map[string]*pendingRequest)
		}
		p.readerDone = make(chan struct{})
		control := p.control
		p.mu.Unlock()
		if control == nil {
			close(p.readerDone)
			return
		}
		go p.readControlLoop(control)
	})
}

func (p *InitProcess) readControlLoop(control *os.File) {
	defer close(p.readerDone)
	for {
		message, files, err := receiveControlMessageFile(control)
		if err != nil {
			closeFiles(files)
			p.mu.Lock()
			p.readerErr = err
			pending := p.pending
			p.pending = nil
			p.mu.Unlock()
			for _, req := range pending {
				close(req.ch)
			}
			return
		}
		p.mu.Lock()
		var req *pendingRequest
		if p.pending != nil {
			req = p.pending[message.ID]
		}
		p.mu.Unlock()
		if req == nil {
			closeFiles(files)
			continue
		}
		select {
		case req.ch <- controlPacket{msg: message, files: files}:
		case <-req.done:
			closeFiles(files)
		}
	}
}

func (p *InitProcess) registerRequest(prefix string, buf int) (string, *pendingRequest, error) {
	p.ensureReader()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.control == nil || p.pending == nil {
		return "", nil, errors.New("native init process is closed")
	}
	p.request++
	id := fmt.Sprintf("%s-%d", prefix, p.request)
	req := &pendingRequest{
		ch:   make(chan controlPacket, buf),
		done: make(chan struct{}),
	}
	p.pending[id] = req
	return id, req, nil
}

func (p *InitProcess) unregisterRequest(id string, req *pendingRequest) {
	if req == nil {
		return
	}
	p.mu.Lock()
	if p.pending != nil && p.pending[id] == req {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	close(req.done)
	for {
		select {
		case pkt, ok := <-req.ch:
			if !ok {
				return
			}
			closeFiles(pkt.files)
		default:
			return
		}
	}
}

func (p *InitProcess) sendRequest(message ControlMessage, files ...*os.File) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	control := p.control
	closed := p.closed
	p.mu.Unlock()
	if closed || control == nil {
		return errors.New("native init process is closed")
	}
	return sendControlMessageFile(control, message, files...)
}

// Exec launches one workload command inside the namespace, streams bounded output
// chunks through emit, and returns the final status.
func (p *InitProcess) Exec(ctx context.Context, request runtime.ExecRequest, emit func(runtime.OutputChunk) error, signals <-chan int) (runtime.ExecResult, error) {
	var empty runtime.ExecResult
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	args := append([]string(nil), request.Argv...)
	if len(args) == 0 && request.Shell != "" {
		args = []string{"/bin/sh", "-lc", request.Shell}
	}
	payload, err := json.Marshal(workloadRequest{Argv: args, Cwd: request.Cwd, Env: request.Env, TTY: request.TTY, TimeoutMS: request.TimeoutMS, OutputLimit: request.OutputLimit})
	if err != nil {
		return empty, fmt.Errorf("encode native workload request: %w", err)
	}
	id, req, err := p.registerRequest("exec", 64)
	if err != nil {
		return empty, err
	}
	defer p.unregisterRequest(id, req)
	if err := p.sendRequest(ControlMessage{Type: "exec", ID: id, Payload: payload}); err != nil {
		return empty, fmt.Errorf("send native workload request: %w", err)
	}
	for {
		select {
		case number := <-signals:
			if err := p.sendExecSignal(id, number); err != nil {
				return empty, err
			}
		case <-ctx.Done():
			_ = p.sendExecSignal(id, int(syscall.SIGKILL))
			return empty, ctx.Err()
		case pkt, ok := <-req.ch:
			if !ok {
				return empty, p.initExitError(fmt.Errorf("read native workload result: %w", p.lastReaderErr()))
			}
			closeFiles(pkt.files)
			message := pkt.msg
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
}

func (p *InitProcess) lastReaderErr() error {
	p.mu.Lock()
	err := p.readerErr
	p.mu.Unlock()
	if err == nil {
		return errors.New("control channel closed")
	}
	return err
}

func (p *InitProcess) initExitError(cause error) error {
	p.mu.Lock()
	p.closed = true
	control := p.control
	p.control = nil
	p.mu.Unlock()
	if control != nil {
		_ = control.Close()
	}
	waitErr := p.waitCmd()
	var stderr string
	if p.stderr != nil {
		stderr = strings.TrimSpace(p.stderr.String())
	}
	if waitErr != nil || stderr != "" {
		return fmt.Errorf("%w (native init exit: %v; stderr: %s)", cause, waitErr, stderr)
	}
	return cause
}

func (p *InitProcess) sendExecSignal(id string, number int) error {
	if number < 1 || number > 64 {
		return fmt.Errorf("signal number %d is outside 1..64", number)
	}
	p.mu.Lock()
	active := p.pending != nil && p.pending[id] != nil
	p.mu.Unlock()
	if id == "" || !active {
		return errors.New("native init process has no active exec")
	}
	payload, err := json.Marshal(struct {
		Signal int `json:"signal"`
	}{Signal: number})
	if err != nil {
		return err
	}
	return p.sendRequest(ControlMessage{Type: "signal", ID: id, Payload: payload})
}

func (p *InitProcess) OpenTerminal(ctx context.Context, request runtime.TerminalRequest) (runtime.Terminal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh"}, Cwd: request.Cwd, Env: request.Env, TTY: true, Rows: request.Rows, Columns: request.Columns})
	if err != nil {
		return nil, err
	}
	id, req, err := p.registerRequest("terminal", 4)
	if err != nil {
		return nil, err
	}
	defer p.unregisterRequest(id, req)
	if err := p.sendRequest(ControlMessage{Type: "terminal", ID: id, Payload: payload}); err != nil {
		return nil, fmt.Errorf("send native terminal request: %w", err)
	}
	cancelled := false
	for {
		var ctxDone <-chan struct{}
		if !cancelled {
			ctxDone = ctx.Done()
		}
		select {
		case <-ctxDone:
			cancelled = true
			_ = p.sendExecSignal(id, int(syscall.SIGTERM))
		case pkt, ok := <-req.ch:
			if !ok {
				return nil, fmt.Errorf("read native terminal response: %w", p.lastReaderErr())
			}
			message, files := pkt.msg, pkt.files
			if message.Type == "error" {
				closeFiles(files)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return nil, errors.New(message.Error)
			}
			if message.Type != "terminal_started" || len(files) != 1 {
				closeFiles(files)
				return nil, fmt.Errorf("unexpected native terminal response %q with %d descriptors", message.Type, len(files))
			}
			if err := ctx.Err(); err != nil {
				closeFiles(files)
				return nil, err
			}
			return newNativeTerminal(files[0], p, id), nil
		}
	}
}

func (p *InitProcess) releaseTerminal(_ string) {}

func (p *InitProcess) controlCall(ctx context.Context, msgType, idPrefix string, payload json.RawMessage, files ...*os.File) (ControlMessage, []*os.File, error) {
	if err := ctx.Err(); err != nil {
		return ControlMessage{}, nil, err
	}
	id, req, err := p.registerRequest(idPrefix, 4)
	if err != nil {
		return ControlMessage{}, nil, err
	}
	defer p.unregisterRequest(id, req)
	if err := p.sendRequest(ControlMessage{Type: msgType, ID: id, Payload: payload}, files...); err != nil {
		return ControlMessage{}, nil, err
	}
	select {
	case <-ctx.Done():
		return ControlMessage{}, nil, ctx.Err()
	case pkt, ok := <-req.ch:
		if !ok {
			return ControlMessage{}, nil, p.initExitError(fmt.Errorf("read native %s response: %w", msgType, p.lastReaderErr()))
		}
		if pkt.msg.Type == "error" {
			closeFiles(pkt.files)
			return ControlMessage{}, nil, errors.New(pkt.msg.Error)
		}
		return pkt.msg, pkt.files, nil
	}
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
	if err := unix.SetNonblock(pair[0], true); err != nil {
		_ = unix.Close(pair[0])
		_ = unix.Close(pair[1])
		return nil, fmt.Errorf("configure nonblocking init control socket: %w", err)
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
	// Keep cellbox-init and cellbox-exec on one Go processor so low pids.max
	// ceilings do not fail Go runtime thread creation inside the sandbox cgroup.
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/", "GOMAXPROCS=1"}
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
		proc := &InitProcess{cmd: command, control: parentSocket, stderr: stderr}
		proc.ensureReader()
		return proc, nil
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
	p.mu.Lock()
	if p.closed {
		control := p.control
		p.control = nil
		readerDone := p.readerDone
		p.mu.Unlock()
		if control != nil {
			_ = control.Close()
		}
		if readerDone != nil {
			<-readerDone
		}
		return nil
	}
	p.closed = true
	control := p.control
	p.control = nil
	command := p.cmd
	readerDone := p.readerDone
	p.mu.Unlock()
	if control != nil {
		_ = control.Close()
	}
	if readerDone != nil {
		<-readerDone
	}
	if command == nil || command.Process == nil {
		return nil
	}
	err := command.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill native init: %w", err)
	}
	waitErr := p.waitCmd()
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
	p.mu.Lock()
	if p.control == nil || p.closed {
		p.mu.Unlock()
		return nil
	}
	command := p.cmd
	p.mu.Unlock()
	_, files, err := p.controlCall(ctx, "shutdown", "shutdown", nil)
	closeFiles(files)
	p.mu.Lock()
	p.closed = true
	control := p.control
	p.control = nil
	readerDone := p.readerDone
	p.mu.Unlock()
	if control != nil {
		_ = control.Close()
	}
	if readerDone != nil {
		<-readerDone
	}
	if command == nil {
		return err
	}
	if err != nil {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = p.waitCmd()
		return fmt.Errorf("send native init shutdown: %w", err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- p.waitCmd() }()
	select {
	case err := <-waitResult:
		if err != nil {
			var stderr string
			if p.stderr != nil {
				stderr = p.stderr.String()
			}
			return fmt.Errorf("native init exited: %w (stderr: %s)", err, stderr)
		}
		return nil
	case <-ctx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-waitResult
		return ctx.Err()
	}
}
