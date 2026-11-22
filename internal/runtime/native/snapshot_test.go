//go:build linux

package native

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCopyOverlayTreePreservesFilesSymlinksAndXattrs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "snapshot")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(source, "nested")
	if err := os.Mkdir(nested, 0750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "hello.txt")
	if err := os.WriteFile(file, []byte("snapshot bytes"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hello.txt", filepath.Join(nested, "link")); err != nil {
		t.Fatal(err)
	}
	xattr := "user.cellbox.snapshot-test"
	if err := unix.Lsetxattr(file, xattr, []byte("metadata"), 0); err != nil {
		t.Logf("filesystem does not support test xattrs: %v", err)
		xattr = ""
	}
	if err := copyOverlayTree(source, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt"))
	if err != nil || string(got) != "snapshot bytes" {
		t.Fatalf("copied file=%q err=%v", got, err)
	}
	info, err := os.Lstat(filepath.Join(destination, "nested", "link"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copied link mode=%v err=%v", info, err)
	}
	if xattr != "" {
		value := make([]byte, 32)
		n, err := unix.Lgetxattr(filepath.Join(destination, "nested", "hello.txt"), xattr, value)
		if err != nil || string(value[:n]) != "metadata" {
			t.Fatalf("copied xattr=%q err=%v", value[:n], err)
		}
	}
}

func TestCopyMergedTreeSkipsRuntimeMounts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "merged")
	destination := filepath.Join(root, "flat")
	for _, dir := range []string{"etc", "proc", "dev", "sys", "run"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, dir, "sentinel"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyMergedTree(source, destination); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "etc", "sentinel")); err != nil || string(got) != "etc" {
		t.Fatalf("rootfs file=%q err=%v", got, err)
	}
	for _, dir := range []string{"proc", "dev", "sys", "run"} {
		if _, err := os.Stat(filepath.Join(destination, dir)); !os.IsNotExist(err) {
			t.Fatalf("runtime mount %s was copied: err=%v", dir, err)
		}
	}
}
