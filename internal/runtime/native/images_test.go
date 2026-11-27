//go:build linux

package native

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
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

func TestParseImageEnvironmentAndSessionOverrides(t *testing.T) {
	imageEnv, err := parseImageEnvironment([]string{"PATH=/usr/local/bin:/usr/bin", "HOME=/root", "EMPTY="})
	if err != nil {
		t.Fatal(err)
	}
	got := mergeEnv(imageEnv, map[string]string{"PATH": "/custom/bin", "CELLBOX_TEST": "1"})
	if got["PATH"] != "/custom/bin" || got["HOME"] != "/root" || got["EMPTY"] != "" || got["CELLBOX_TEST"] != "1" {
		t.Fatalf("merged image and session environment = %#v", got)
	}
	for _, invalid := range []string{"", "NO_EQUALS", "=empty-key", "BAD\x00KEY=value", "BAD=value\x00suffix"} {
		if _, err := parseImageEnvironment([]string{invalid}); err == nil {
			t.Errorf("accepted invalid OCI environment value %q", invalid)
		}
	}
}

func TestImageStoreEnsureUsesResolvedCache(t *testing.T) {
	store := newImageStore(t.TempDir())
	store.cache["alpine:latest"] = imageStoreResult{
		info:   cellruntime.ImageInfo{Reference: "alpine:latest", Digest: "sha256:cached"},
		rootfs: "/cached/rootfs",
		env:    map[string]string{"PATH": "/bin"},
	}
	info, rootfs, env, err := store.Ensure(context.Background(), "alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	if info.Digest != "sha256:cached" || rootfs != "/cached/rootfs" || env["PATH"] != "/bin" {
		t.Fatalf("cached image result: info=%+v rootfs=%q env=%v", info, rootfs, env)
	}
	env["PATH"] = "/mutated"
	_, _, nextEnv, err := store.Ensure(context.Background(), "alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	if nextEnv["PATH"] != "/bin" {
		t.Fatalf("caller mutated cached image environment: %v", nextEnv)
	}
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

func TestImageStoreListAndRemove(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := newImageStore(root)

	empty, err := store.List(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty List = %v, %v", empty, err)
	}

	hexA := strings.Repeat("a", 64)
	dirA := filepath.Join(root, "images", hexA)
	rootfsA := filepath.Join(dirA, "rootfs")
	if err := os.MkdirAll(rootfsA, 0755); err != nil {
		t.Fatal(err)
	}
	if err := store.recordMeta(dirA, "sha256:"+hexA, 1234, "alpine:3.20"); err != nil {
		t.Fatal(err)
	}
	if err := store.recordMeta(dirA, "sha256:"+hexA, 1234, "alpine:latest"); err != nil {
		t.Fatal(err)
	}
	store.cache["alpine:3.20"] = imageStoreResult{rootfs: rootfsA}
	store.cache["alpine:latest"] = imageStoreResult{rootfs: rootfsA}
	store.order = []string{"alpine:3.20", "alpine:latest"}

	hexB := strings.Repeat("b", 64)
	rootfsB := filepath.Join(root, "images", hexB, "rootfs")
	if err := os.MkdirAll(rootfsB, 0755); err != nil {
		t.Fatal(err)
	}

	images, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 3 {
		t.Fatalf("List count = %d, want 3: %+v", len(images), images)
	}
	if images[0].Reference != "alpine:3.20" || images[1].Reference != "alpine:latest" || images[2].Reference != "sha256:"+hexB {
		t.Fatalf("unexpected image list order/refs: %+v", images)
	}

	if err := store.Remove(ctx, "alpine:3.20", func(got string) bool { return got == rootfsA }); err == nil {
		t.Fatal("expected error removing in-use image")
	}

	// Removing one tag when two exist keeps the rootfs and other tag.
	if err := store.Remove(ctx, "alpine:3.20", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rootfsA); err != nil {
		t.Fatalf("rootfsA should still exist: %v", err)
	}
	if _, ok := store.cache["alpine:3.20"]; ok {
		t.Fatal("expected alpine:3.20 evicted from cache")
	}

	// Removing the last tag deletes the rootfs directory.
	if err := store.Remove(ctx, "alpine:latest", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirA); !os.IsNotExist(err) {
		t.Fatalf("expected dirA deleted, stat err = %v", err)
	}

	// Removing by digest deletes untagged image.
	if err := store.Remove(ctx, "sha256:"+hexB, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "images", hexB)); !os.IsNotExist(err) {
		t.Fatalf("expected dirB deleted, stat err = %v", err)
	}

	if err := store.Remove(ctx, "missing:latest", nil); err == nil {
		t.Fatal("expected error removing missing image")
	}
}
