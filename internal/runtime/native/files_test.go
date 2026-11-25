//go:build linux

package native

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSandboxFilePathStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "work"), 0700); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	fd, err := openSandboxPath(rootFD, "work/value", unix.O_CREAT|unix.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sandboxPath("../../etc/passwd"); err == nil {
		t.Fatal("accepted a path outside the sandbox")
	}
	if _, err := openSandboxPath(rootFD, "outside/pwn", unix.O_CREAT|unix.O_WRONLY, 0600); err == nil {
		t.Fatal("followed an absolute symlink outside the sandbox")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn")); !os.IsNotExist(err) {
		t.Fatalf("wrote outside the sandbox: %v", err)
	}
}

func TestNativeFileReadsRejectSpecialFilesAndWritesAreAtomic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)

	if fd, _, err := openSandboxRegularRead(rootFD, "pipe"); err == nil {
		unix.Close(fd)
		t.Fatal("accepted a FIFO as a regular file")
	}
	reader := errorAfterBytes{}
	if err := writeSandboxFileAtomic(rootFD, "value", reader, 0644); !errors.Is(err, errTestRead) {
		t.Fatalf("write error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "value"))
	if err != nil || string(got) != "original" {
		t.Fatalf("existing file after failed write = %q, err = %v", got, err)
	}
	if err := writeSandboxFileAtomic(rootFD, "value", bytes.NewBufferString("updated"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(root, "value"))
	if err != nil || string(got) != "updated" {
		t.Fatalf("existing file after successful write = %q, err = %v", got, err)
	}
}

var errTestRead = errors.New("test reader failure")

type errorAfterBytes struct{}

func (errorAfterBytes) Read(buffer []byte) (int, error) {
	copy(buffer, "partial")
	return len("partial"), errTestRead
}

func TestNativeArchiveExtractAndValidateBeforeWriting(t *testing.T) {
	root := t.TempDir()
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, entry := range []struct {
		name string
		data string
	}{{"nested/value.txt", "restored"}} {
		if err := writer.WriteHeader(&tar.Header{Name: entry.name, Mode: 0640, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeArchive(bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("validate archive: %v", err)
	}
	if err := extractNativeArchive(context.Background(), rootFD, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "nested", "value.txt"))
	if err != nil || string(got) != "restored" {
		t.Fatalf("extracted file = %q, err = %v", got, err)
	}

	var malicious bytes.Buffer
	writer = tar.NewWriter(&malicious)
	if err := writer.WriteHeader(&tar.Header{Name: "partial.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("bad"))
	if err := writer.WriteHeader(&tar.Header{Name: "../../escape", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeArchive(bytes.NewReader(malicious.Bytes())); err == nil {
		t.Fatal("accepted archive traversal")
	}
	if _, err := os.Stat(filepath.Join(root, "partial.txt")); !os.IsNotExist(err) {
		t.Fatalf("validation wrote a partial entry: %v", err)
	}

	var symEscape bytes.Buffer
	writer = tar.NewWriter(&symEscape)
	_ = writer.WriteHeader(&tar.Header{Name: "pre.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte("bad"))
	_ = writer.WriteHeader(&tar.Header{Name: "sub", Typeflag: tar.TypeSymlink, Linkname: "/etc"})
	_ = writer.WriteHeader(&tar.Header{Name: "sub/evil", Mode: 0600, Size: 4, Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte("evil"))
	_ = writer.Close()
	if err := validateNativeArchive(bytes.NewReader(symEscape.Bytes())); err == nil {
		t.Fatal("accepted archive entry traversing symlink created in archive")
	}
}
