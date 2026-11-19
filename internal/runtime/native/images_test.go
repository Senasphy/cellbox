//go:build linux

package native

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarLayer(t *testing.T, entries ...tar.Header) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		if err := writer.WriteHeader(&entry); err != nil {
			t.Fatal(err)
		}
		if entry.Typeflag == tar.TypeReg {
			content := strings.Repeat("x", int(entry.Size))
			if _, err := io.WriteString(writer, content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buffer
}

func TestApplyImageLayerWhiteoutsAndSafeExtraction(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "opt"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt", "stale"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	layer := tarLayer(t,
		tar.Header{Name: "etc/.wh.old", Typeflag: tar.TypeReg, Mode: 0600},
		tar.Header{Name: "opt/.wh..wh..opq", Typeflag: tar.TypeReg, Mode: 0600},
		tar.Header{Name: "locked", Typeflag: tar.TypeDir, Mode: 0500},
		tar.Header{Name: "locked/data", Typeflag: tar.TypeReg, Mode: 0600, Size: 1},
		tar.Header{Name: "usr/bin/tool", Typeflag: tar.TypeReg, Mode: 04755, Size: 4},
	)
	if _, err := applyImageLayer(root, layer, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "old")); !os.IsNotExist(err) {
		t.Fatalf("whiteouted file remains: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "opt")); err != nil || len(entries) != 0 {
		t.Fatalf("opaque whiteout entries=%v err=%v", entries, err)
	}
	info, err := os.Stat(filepath.Join(root, "usr", "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("setuid bits not stripped: mode=%#o", info.Mode().Perm())
	}
	if info, err := os.Stat(filepath.Join(root, "locked")); err != nil || info.Mode().Perm() != 0500 {
		t.Fatalf("directory mode restoration: info=%v err=%v", info, err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0700) })
}

func TestApplyImageLayerRejectsTraversalAndSymlinkParent(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../escape", "\\outside"} {
		layer := tarLayer(t, tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: 1})
		if _, err := applyImageLayer(root, layer, 0); err == nil {
			t.Fatalf("accepted unsafe archive path %q", name)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	layer := tarLayer(t, tar.Header{Name: "escape/pwn", Typeflag: tar.TypeReg, Mode: 0600, Size: 1})
	if _, err := applyImageLayer(root, layer, 0); err == nil {
		t.Fatal("followed a symlink while extracting an image")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn")); !os.IsNotExist(err) {
		t.Fatalf("wrote through symlink: %v", err)
	}
}
