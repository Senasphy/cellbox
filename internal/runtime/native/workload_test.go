//go:build linux

package native

import (
	"encoding/json"
	"strings"
	"syscall"
	"testing"

	"github.com/senasphy/cellbox/internal/runtime"
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
	if result.ExitCode != 124 || result.Signal != int(syscall.SIGKILL) {
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
