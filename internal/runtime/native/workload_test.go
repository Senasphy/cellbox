//go:build linux

package native

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

func TestRunWorkloadCapturesBoundedOutputAndExitStatus(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh", "-c", "printf '0123456789'; exit 7"}, OutputLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := runWorkload(request)
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 || !result.Truncated || result.OutputBytes != 10 {
		t.Fatalf("result = %+v", result)
	}
	var output strings.Builder
	for _, chunk := range result.Chunks {
		output.Write(chunk.Data)
	}
	if output.String() != "01234567" || len(result.Chunks) == 0 || result.Chunks[0].Stream != "stdout" {
		t.Fatalf("captured output = %q", output.String())
	}
}

func TestRunWorkloadRejectsUnsafeWorkingDirectory(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"true"}, Cwd: "/work/../etc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWorkload(request); err == nil || !strings.Contains(err.Error(), "clean absolute sandbox path") {
		t.Fatalf("runWorkload error = %v", err)
	}
}

func TestRunWorkloadKillsTimedOutProcessGroup(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh", "-c", "sleep 2"}, TimeoutMS: 30})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := runWorkload(request)
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 124 || result.Signal != int(syscall.SIGTERM) || !result.TimedOut {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunWorkloadStreamsOutputWithoutBufferingResponseChunks(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh", "-c", "printf out; printf err >&2"}})
	if err != nil {
		t.Fatal(err)
	}
	streams := map[string]*strings.Builder{"stdout": {}, "stderr": {}}
	payload, err := runWorkloadStreaming(request, func(chunk runtime.OutputChunk) error {
		streams[chunk.Stream].Write(chunk.Data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if streams["stdout"].String() != "out" || streams["stderr"].String() != "err" {
		t.Fatalf("streamed stdout = %q, stderr = %q", streams["stdout"].String(), streams["stderr"].String())
	}
	if len(result.Chunks) != 0 || result.OutputBytes != int64(len("outerr")) {
		t.Fatalf("streaming response retained chunks: %+v", result)
	}
}

func TestRunWorkloadHelperAppliesCwdAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	request, err := json.Marshal(workloadRequest{
		Argv: []string{"/bin/sh", "-c", "printf '%s:' \"$CELLBOX_HELPER_VALUE\"; /bin/pwd"},
		Cwd:  dir,
		Env:  map[string]string{"CELLBOX_HELPER_VALUE": "ready"},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := runWorkload(request)
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for _, chunk := range result.Chunks {
		output.Write(chunk.Data)
	}
	if got, want := strings.TrimSpace(output.String()), "ready:"+filepath.Clean(dir); result.ExitCode != 0 || got != want {
		t.Fatalf("helper output = %q, exit = %d; want %q", got, result.ExitCode, want)
	}
}

func TestRunWorkloadHelperReportsExecFailure(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"cellbox-command-that-does-not-exist"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWorkload(request); err == nil {
		t.Fatal("missing command unexpectedly executed")
	}
}

func TestRunWorkloadPTYAttachesControllingTerminal(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sh", "-c", "printf tty-output"}, TTY: true, Rows: 28, Columns: 96})
	if err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 1)
	payload, err := runWorkloadWithCallbacks(request, nil, nil, func(master *os.File) error {
		fd, err := unix.Dup(int(master.Fd()))
		if err != nil {
			return err
		}
		reader := os.NewFile(uintptr(fd), "pty-test-reader")
		go func() {
			defer reader.Close()
			var result strings.Builder
			buffer := make([]byte, 1024)
			for {
				n, readErr := reader.Read(buffer)
				result.Write(buffer[:n])
				if errors.Is(readErr, unix.EIO) || errors.Is(readErr, io.EOF) {
					break
				}
				if readErr != nil {
					break
				}
			}
			output <- result.String()
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("PTY workload result = %+v", result)
	}
	if got := <-output; got != "tty-output" {
		t.Fatalf("PTY output = %q", got)
	}
}

func TestRunWorkloadSignalsProcessGroup(t *testing.T) {
	request, err := json.Marshal(workloadRequest{Argv: []string{"/bin/sleep", "5"}})
	if err != nil {
		t.Fatal(err)
	}
	signals := make(chan int, 1)
	timer := time.AfterFunc(50*time.Millisecond, func() { signals <- int(syscall.SIGTERM) })
	defer timer.Stop()
	payload, err := runWorkloadWithSignals(request, nil, signals)
	if err != nil {
		t.Fatal(err)
	}
	var result workloadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Signal != int(syscall.SIGTERM) {
		t.Fatalf("signal result = %+v", result)
	}
}

func TestFilterInternalHelperEnvPreservesExplicitOverrides(t *testing.T) {
	base := filterInternalHelperEnv([]string{"PATH=/usr/bin:/bin", "GOMAXPROCS=1", "HOME=/"})
	env := mergeWorkloadEnvironment(base, nil)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOMAXPROCS=") {
			t.Fatalf("internal GOMAXPROCS leaked into workload env: %v", env)
		}
	}
	override := mergeWorkloadEnvironment(base, map[string]string{"GOMAXPROCS": "4"})
	found := false
	for _, entry := range override {
		if entry == "GOMAXPROCS=4" {
			found = true
		}
	}
	if !found {
		t.Fatalf("explicit GOMAXPROCS override missing: %v", override)
	}
}
