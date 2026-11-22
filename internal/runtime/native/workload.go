//go:build linux

package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

const (
	maxWorkloadOutput = runtime.MaxExecOutputBytes
	maxWorkloadArg    = 32 << 10
	maxWorkloadArgs   = 256
	workloadChunkSize = 16 << 10
)

type workloadRequest struct {
	Argv        []string          `json:"argv"`
	Cwd         string            `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TTY         bool              `json:"tty,omitempty"`
	Rows        uint16            `json:"rows,omitempty"`
	Columns     uint16            `json:"columns,omitempty"`
	TimeoutMS   int               `json:"timeout_ms,omitempty"`
	OutputLimit int64             `json:"output_limit,omitempty"`
}

type workloadResponse struct {
	ExitCode    int                   `json:"exit_code"`
	Signal      int                   `json:"signal,omitempty"`
	TimedOut    bool                  `json:"timed_out,omitempty"`
	DurationMS  int64                 `json:"duration_ms"`
	OutputBytes int64                 `json:"output_bytes"`
	Truncated   bool                  `json:"truncated,omitempty"`
	Chunks      []runtime.OutputChunk `json:"chunks,omitempty"`
}

// runWorkload executes one validated command inside the already-pivoted root.
// Output is capped before it enters the control channel.
func runWorkload(payload json.RawMessage) (json.RawMessage, error) {
	return runWorkloadWithSignals(payload, nil, nil)
}

func runWorkloadStreaming(payload json.RawMessage, emit func(runtime.OutputChunk) error) (json.RawMessage, error) {
	return runWorkloadWithSignals(payload, emit, nil)
}

func runWorkloadWithSignals(payload json.RawMessage, emit func(runtime.OutputChunk) error, signals <-chan int) (json.RawMessage, error) {
	return runWorkloadWithCallbacks(payload, emit, signals, nil)
}

func runWorkloadWithCallbacks(payload json.RawMessage, emit func(runtime.OutputChunk) error, signals <-chan int, onTerminal func(*os.File) error) (json.RawMessage, error) {
	var request workloadRequest
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("decode workload request: %w", err)
	}
	if len(request.Argv) == 0 || len(request.Argv) > maxWorkloadArgs {
		return nil, errors.New("workload argv must contain between 1 and 256 arguments")
	}
	for _, arg := range request.Argv {
		if len(arg) > maxWorkloadArg || strings.IndexByte(arg, 0) >= 0 {
			return nil, errors.New("workload argument is too large or contains NUL")
		}
	}
	for key, value := range request.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.IndexByte(value, 0) >= 0 || len(key)+len(value) > maxWorkloadArg {
			return nil, fmt.Errorf("invalid workload environment entry %q", key)
		}
	}
	if request.Cwd != "" {
		if !path.IsAbs(request.Cwd) || path.Clean(request.Cwd) != request.Cwd || strings.IndexByte(request.Cwd, 0) >= 0 {
			return nil, errors.New("workload cwd must be a clean absolute sandbox path")
		}
	}
	if request.TimeoutMS < 0 || request.TimeoutMS > 24*60*60*1000 {
		return nil, errors.New("workload timeout must be between 0 and 86400000 milliseconds")
	}
	outputLimit := request.OutputLimit
	if outputLimit == 0 {
		outputLimit = maxWorkloadOutput
	}
	if outputLimit < 0 || outputLimit > maxWorkloadOutput {
		return nil, fmt.Errorf("workload output limit must be between 0 and %d bytes", maxWorkloadOutput)
	}

	configRead, configWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create exec helper request pipe: %w", err)
	}
	execErrorRead, execErrorWrite, err := os.Pipe()
	if err != nil {
		configRead.Close()
		configWrite.Close()
		return nil, fmt.Errorf("create exec helper error pipe: %w", err)
	}
	helperRequest, err := json.Marshal(execHelperRequest{Argv: request.Argv, Cwd: request.Cwd, Env: request.Env})
	if err != nil {
		configRead.Close()
		configWrite.Close()
		execErrorRead.Close()
		execErrorWrite.Close()
		return nil, fmt.Errorf("encode exec helper request: %w", err)
	}
	var master, slave *os.File
	if request.TTY {
		if onTerminal == nil {
			configRead.Close()
			configWrite.Close()
			execErrorRead.Close()
			execErrorWrite.Close()
			return nil, errors.New("terminal workload has no master descriptor receiver")
		}
		master, slave, err = openNativePTY(request.Rows, request.Columns)
		if err != nil {
			configRead.Close()
			configWrite.Close()
			execErrorRead.Close()
			execErrorWrite.Close()
			return nil, err
		}
	}
	helperPath := "/run/cellbox/cellbox-exec"
	if _, err := os.Stat(helperPath); err != nil {
		// Unit tests execute outside the sandbox root where the helper is mounted.
		helperPath = "/proc/self/exe"
	}
	command := exec.Command(helperPath)
	command.Args = []string{"cellbox-exec"}
	command.Dir = "/"
	command.Env = os.Environ()
	command.ExtraFiles = []*os.File{configRead, execErrorWrite}
	if request.TTY {
		command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	} else {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	command.WaitDelay = 2 * time.Second
	collector := &workloadOutput{limit: outputLimit}
	if !request.TTY {
		command.Stdout = outputStream{collector: collector, stream: "stdout", emit: emit}
		command.Stderr = outputStream{collector: collector, stream: "stderr", emit: emit}
	}
	started := time.Now()
	if err := startTrackedHelper(command); err != nil {
		configRead.Close()
		configWrite.Close()
		execErrorRead.Close()
		execErrorWrite.Close()
		if master != nil {
			master.Close()
			slave.Close()
		}
		return nil, fmt.Errorf("start workload: %w", err)
	}
	_ = configRead.Close()
	_ = execErrorWrite.Close()
	if slave != nil {
		_ = slave.Close()
	}
	if _, err := configWrite.Write(helperRequest); err != nil {
		_ = configWrite.Close()
		_ = command.Process.Kill()
		_ = waitTrackedHelper(command)
		_ = execErrorRead.Close()
		if master != nil {
			_ = master.Close()
		}
		return nil, fmt.Errorf("send exec helper request: %w", err)
	}
	_ = configWrite.Close()
	execError, readErr := io.ReadAll(execErrorRead)
	_ = execErrorRead.Close()
	if readErr != nil {
		_ = command.Process.Kill()
		_ = waitTrackedHelper(command)
		if master != nil {
			_ = master.Close()
		}
		return nil, fmt.Errorf("read exec helper error: %w", readErr)
	}
	if len(execError) > 0 {
		_ = waitTrackedHelper(command)
		if master != nil {
			_ = master.Close()
		}
		return nil, fmt.Errorf("exec workload: %s", strings.TrimSpace(string(execError)))
	}
	if master != nil {
		if err := onTerminal(master); err != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			_ = waitTrackedHelper(command)
			_ = master.Close()
			return nil, fmt.Errorf("send PTY master descriptor: %w", err)
		}
		_ = master.Close()
	}
	signalDone := make(chan struct{})
	signalStopped := make(chan struct{})
	var processMu sync.Mutex
	processRunning := true
	if signals != nil {
		go func(pid int) {
			defer close(signalStopped)
			select {
			case number := <-signals:
				if number > 0 && number < 65 {
					processMu.Lock()
					if processRunning {
						_ = syscall.Kill(-pid, syscall.Signal(number))
					}
					processMu.Unlock()
				}
			case <-signalDone:
			}
		}(command.Process.Pid)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- waitTrackedHelper(command) }()
	timedOut := false
	if request.TimeoutMS > 0 {
		timer := time.NewTimer(time.Duration(request.TimeoutMS) * time.Millisecond)
		select {
		case err = <-waitResult:
			timer.Stop()
		case <-timer.C:
			timedOut = true
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			grace := time.NewTimer(2 * time.Second)
			select {
			case err = <-waitResult:
				grace.Stop()
			case <-grace.C:
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
				err = <-waitResult
			}
		}
	} else {
		err = <-waitResult
	}
	processMu.Lock()
	processRunning = false
	processMu.Unlock()
	close(signalDone)
	if signals != nil {
		<-signalStopped
	}
	response := workloadResponse{DurationMS: time.Since(started).Milliseconds(), OutputBytes: collector.total, Truncated: collector.truncated}
	if emit == nil {
		response.Chunks = collector.chunks
	}
	if timedOut {
		response.ExitCode = 124
		response.TimedOut = true
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				response.Signal = int(status.Signal())
			}
		}
	} else if err == nil {
		response.ExitCode = 0
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			response.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				response.Signal = int(status.Signal())
			}
		} else {
			return nil, fmt.Errorf("start workload: %w", err)
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode workload result: %w", err)
	}
	return encoded, nil
}

type workloadOutput struct {
	mu        sync.Mutex
	limit     int64
	total     int64
	retained  int64
	truncated bool
	chunks    []runtime.OutputChunk
}

type outputStream struct {
	collector *workloadOutput
	stream    string
	emit      func(runtime.OutputChunk) error
}

func (w outputStream) Write(data []byte) (int, error) {
	originalLength := len(data)
	w.collector.mu.Lock()
	defer w.collector.mu.Unlock()
	w.collector.total += int64(len(data))
	remaining := w.collector.limit - w.collector.retained
	if remaining <= 0 {
		w.collector.truncated = true
		return len(data), nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		w.collector.truncated = true
	}
	w.collector.retained += int64(len(data))
	for len(data) > 0 {
		n := len(data)
		if n > workloadChunkSize {
			n = workloadChunkSize
		}
		chunk := runtime.OutputChunk{Stream: w.stream, Data: data[:n]}
		if w.emit != nil {
			if err := w.emit(chunk); err != nil {
				return 0, err
			}
		} else {
			chunk.Data = append([]byte(nil), chunk.Data...)
			w.collector.chunks = append(w.collector.chunks, chunk)
		}
		data = data[n:]
	}
	return originalLength, nil
}
