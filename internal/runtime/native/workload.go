//go:build linux

package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	maxWorkloadOutput = 512 << 10
	maxWorkloadArg    = 32 << 10
	maxWorkloadArgs   = 256
	workloadChunkSize = 16 << 10
)

type workloadRequest struct {
	Argv        []string          `json:"argv"`
	Cwd         string            `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutMS   int               `json:"timeout_ms,omitempty"`
	OutputLimit int64             `json:"output_limit,omitempty"`
}

type workloadResponse struct {
	ExitCode    int                   `json:"exit_code"`
	Signal      int                   `json:"signal,omitempty"`
	DurationMS  int64                 `json:"duration_ms"`
	OutputBytes int64                 `json:"output_bytes"`
	Truncated   bool                  `json:"truncated,omitempty"`
	Chunks      []runtime.OutputChunk `json:"chunks,omitempty"`
}

// runWorkload executes one validated command inside the already-pivoted root.
// Output is capped before it enters the control channel.
func runWorkload(payload json.RawMessage) (json.RawMessage, error) {
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

	ctx := context.Background()
	cancel := func() {}
	if request.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	command := exec.CommandContext(ctx, request.Argv[0], request.Argv[1:]...)
	command.Dir = request.Cwd
	command.Env = os.Environ()
	for key, value := range request.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	collector := &workloadOutput{limit: outputLimit}
	command.Stdout = outputStream{collector: collector, stream: "stdout"}
	command.Stderr = outputStream{collector: collector, stream: "stderr"}
	started := time.Now()
	err := command.Run()
	response := workloadResponse{DurationMS: time.Since(started).Milliseconds(), OutputBytes: collector.total, Truncated: collector.truncated, Chunks: collector.chunks}
	if err == nil {
		response.ExitCode = 0
	} else if ctx.Err() != nil {
		response.ExitCode = 124
		response.Signal = int(syscall.SIGKILL)
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
		w.collector.chunks = append(w.collector.chunks, runtime.OutputChunk{Stream: w.stream, Data: append([]byte(nil), data[:n]...)})
		data = data[n:]
	}
	return originalLength, nil
}
