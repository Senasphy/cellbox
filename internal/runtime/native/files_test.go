//go:build linux

package native

import (
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
