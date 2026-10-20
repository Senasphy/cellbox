// Package contract contains reusable conformance checks for runtime backends.
package contract

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

// RunBasic checks the backend operations shared by the Docker and native runtimes.
// The image must provide /bin/sh and the basic POSIX utilities used below.
func RunBasic(t testing.TB, backend runtime.Runtime, image string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resolved, err := backend.EnsureImage(ctx, image)
	if err != nil {
		t.Fatalf("EnsureImage(%q): %v", image, err)
	}
	box, err := backend.Create(ctx, runtime.SandboxSpec{
		ID: "ses_runtime_contract_" + time.Now().UTC().Format("20060102150405.000000000"), Image: resolved,
		Limits:      runtime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64},
		NetworkMode: "none",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := box.Destroy(cleanupCtx); err != nil {
			t.Errorf("Destroy: %v", err)
		}
	}()
	if box.ID() == "" {
		t.Fatal("Create returned an empty sandbox ID")
	}

	handle, err := box.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/sh", "-c", "printf contract-output; exit 7"}, TimeoutMS: 10_000, OutputLimit: 4096})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	var output bytes.Buffer
	for chunk := range handle.Output() {
		output.Write(chunk.Data)
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Exec.Wait: %v", err)
	}
	if result.ExitCode != 7 || output.String() != "contract-output" {
		t.Fatalf("exec result=%+v output=%q", result, output.String())
	}

	handle, err = box.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/sh", "-c", "printf contract-stdout; printf contract-stderr >&2"}, TimeoutMS: 10_000, OutputLimit: 4096})
	if err != nil {
		t.Fatalf("Exec stdout/stderr: %v", err)
	}
	streams := map[string]string{}
	for chunk := range handle.Output() {
		streams[chunk.Stream] += string(chunk.Data)
	}
	result, err = handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Exec stdout/stderr Wait: %v", err)
	}
	if result.ExitCode != 0 || streams["stdout"] != "contract-stdout" || streams["stderr"] != "contract-stderr" {
		t.Fatalf("stream result=%+v streams=%v", result, streams)
	}

	handle, err = box.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/sh", "-c", "printf contract-large-output >/dev/null; i=0; while [ \"$i\" -lt 1024 ]; do printf x; i=$((i+1)); done"}, TimeoutMS: 10_000, OutputLimit: 128})
	if err != nil {
		t.Fatalf("Exec output cap: %v", err)
	}
	var capped bytes.Buffer
	for chunk := range handle.Output() {
		capped.Write(chunk.Data)
	}
	result, err = handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Exec output cap Wait: %v", err)
	}
	if result.ExitCode != 0 || result.OutputBytes != 1024 || !result.Truncated || capped.Len() != 128 {
		t.Fatalf("output cap result=%+v visible=%d", result, capped.Len())
	}

	if _, _, err := box.ReadFile(ctx, "/../../etc/passwd"); err == nil {
		t.Fatal("ReadFile accepted a path outside the sandbox")
	}
	if err := box.WriteFile(ctx, "/../escape", bytes.NewBufferString("bad"), 0600); err == nil {
		t.Fatal("WriteFile accepted a path outside the sandbox")
	}
	if _, err := box.Archive(ctx, "/../../"); err == nil {
		t.Fatal("Archive accepted a path outside the sandbox")
	}
	var traversal bytes.Buffer
	traversalWriter := tar.NewWriter(&traversal)
	if err := traversalWriter.WriteHeader(&tar.Header{Name: "partial.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = traversalWriter.Write([]byte("bad"))
	if err := traversalWriter.WriteHeader(&tar.Header{Name: "../../escape", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = traversalWriter.Write([]byte("bad"))
	if err := traversalWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := box.Extract(ctx, "/tmp", bytes.NewReader(traversal.Bytes())); err == nil {
		t.Fatal("Extract accepted a traversal archive")
	}
	if _, _, err := box.ReadFile(ctx, "/tmp/partial.txt"); err == nil {
		t.Fatal("Extract wrote an entry before rejecting a later traversal entry")
	}

	const file = "/tmp/cellbox-contract.txt"
	if err := box.WriteFile(ctx, file, bytes.NewBufferString("file-contract"), 0640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	read, info, err := box.ReadFile(ctx, file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data, readErr := io.ReadAll(read)
	closeErr := read.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read file: read=%v close=%v", readErr, closeErr)
	}
	if string(data) != "file-contract" || info.Size != int64(len(data)) {
		t.Fatalf("read file data=%q info=%+v", data, info)
	}
	archived, err := box.Archive(ctx, "/tmp")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	tr := tar.NewReader(archived)
	foundInArchive := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = archived.Close()
			t.Fatalf("read archive: %v", err)
		}
		if path.Base(header.Name) == path.Base(file) {
			archivedData, readErr := io.ReadAll(tr)
			if readErr != nil {
				_ = archived.Close()
				t.Fatalf("read archived file: %v", readErr)
			}
			foundInArchive = string(archivedData) == "file-contract"
		}
	}
	if err := archived.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if !foundInArchive {
		t.Fatal("Archive(/tmp) omitted the file written by WriteFile")
	}

	entries, err := box.ListDir(ctx, "/tmp")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Path == file && !entry.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListDir(/tmp) omitted %s: %+v", file, entries)
	}

	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "restored.txt", Mode: 0600, Size: int64(len("archive-contract")), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("archive-contract")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := box.Extract(ctx, "/tmp", bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	restored, _, err := box.ReadFile(ctx, "/tmp/restored.txt")
	if err != nil {
		t.Fatalf("ReadFile after Extract: %v", err)
	}
	restoredBytes, readErr := io.ReadAll(restored)
	_ = restored.Close()
	if readErr != nil || string(restoredBytes) != "archive-contract" {
		t.Fatalf("extracted file=%q read error=%v", restoredBytes, readErr)
	}
	if err := box.DeleteFile(ctx, file); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, _, err := box.ReadFile(ctx, file); err == nil {
		t.Fatal("ReadFile succeeded after DeleteFile")
	}

	updated := runtime.Limits{CPUMillicores: 500, MemoryMB: 192, PIDs: 48}
	if err := box.UpdateLimits(ctx, updated); err != nil {
		t.Fatalf("UpdateLimits: %v", err)
	}
	if _, err := box.Stats(ctx); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if backend.Capabilities().TTY {
		terminal, err := box.OpenTerminal(ctx, runtime.TerminalRequest{Columns: 80, Rows: 24})
		if err != nil {
			t.Fatalf("OpenTerminal: %v", err)
		}
		output := make(chan string, 1)
		go func() {
			var collected strings.Builder
			buffer := make([]byte, 1024)
			for {
				n, readErr := terminal.Read(buffer)
				if n > 0 {
					collected.Write(buffer[:n])
					if strings.Contains(collected.String(), "contract-terminal-ok") {
						output <- collected.String()
						return
					}
				}
				if readErr != nil {
					output <- "read error: " + readErr.Error()
					return
				}
			}
		}()
		if _, err := terminal.Write([]byte("printf contract-terminal-ok\r")); err != nil {
			terminal.Close()
			t.Fatalf("terminal input: %v", err)
		}
		select {
		case output := <-output:
			if !strings.Contains(output, "contract-terminal-ok") {
				terminal.Close()
				t.Fatalf("terminal output=%q", output)
			}
		case <-time.After(10 * time.Second):
			terminal.Close()
			t.Fatal("terminal did not return command output")
		}
		if err := terminal.Resize(ctx, 100, 40); err != nil {
			terminal.Close()
			t.Fatalf("terminal resize: %v", err)
		}
		if err := terminal.Close(); err != nil {
			t.Errorf("terminal close: %v", err)
		}
	}
	timed, err := box.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/sh", "-c", ": contract-timeout; while :; do :; done"}, TimeoutMS: 100, OutputLimit: 1024})
	if err != nil {
		t.Fatalf("Exec timeout: %v", err)
	}
	for range timed.Output() {
	}
	timeoutResult, timeoutErr := timed.Wait(ctx)
	if !errors.Is(timeoutErr, context.DeadlineExceeded) || timeoutResult.Signal != 9 {
		t.Fatalf("timeout result=%+v error=%v", timeoutResult, timeoutErr)
	}
	if err := box.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := box.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/true"}, TimeoutMS: 1000, OutputLimit: 1024}); err == nil {
		t.Fatal("Exec succeeded after Kill")
	}
}
