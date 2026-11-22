//go:build linux

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/id"
	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

func (s *sandbox) Snapshot(ctx context.Context) (cellruntime.SnapshotInfo, error) {
	return s.snapshot(ctx, false)
}

func (s *sandbox) SnapshotFlatten(ctx context.Context) (cellruntime.SnapshotInfo, error) {
	return s.snapshot(ctx, true)
}

func (s *sandbox) snapshot(ctx context.Context, flatten bool) (cellruntime.SnapshotInfo, error) {
	if err := ctx.Err(); err != nil {
		return cellruntime.SnapshotInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || s.killed {
		return cellruntime.SnapshotInfo{}, errors.New("native sandbox is not available for snapshot")
	}
	if err := s.cgroup.Freeze(); err != nil {
		return cellruntime.SnapshotInfo{}, err
	}
	thawed := false
	defer func() {
		if !thawed {
			_ = writeCgroupValue(s.cgroup.path, "cgroup.freeze", "0")
		}
	}()
	if err := waitCgroupFrozen(ctx, s.cgroup.path); err != nil {
		return cellruntime.SnapshotInfo{}, err
	}
	snapshotID, err := id.New("snp")
	if err != nil {
		return cellruntime.SnapshotInfo{}, err
	}
	destination := filepath.Join(s.runtime.root, "snapshots", snapshotID)
	if err := os.Mkdir(destination, 0700); err != nil {
		return cellruntime.SnapshotInfo{}, fmt.Errorf("create snapshot directory: %w", err)
	}
	upper := filepath.Join(destination, "upper")
	var copyErr error
	if flatten {
		merged := fmt.Sprintf("/proc/%d/root", s.init.cmd.Process.Pid)
		copyErr = copyMergedTree(merged, upper)
		if copyErr == nil {
			copyErr = unix.Lsetxattr(upper, "user.overlay.opaque", []byte("y"), 0)
		}
	} else {
		copyErr = copyOverlayTree(filepath.Join(s.dir, "upper"), upper)
	}
	if copyErr != nil {
		_ = os.RemoveAll(destination)
		return cellruntime.SnapshotInfo{}, fmt.Errorf("copy frozen overlay snapshot: %w", copyErr)
	}
	size, err := upperDirectoryUsage(upper, nil, 0)
	if err != nil {
		_ = os.RemoveAll(destination)
		return cellruntime.SnapshotInfo{}, fmt.Errorf("measure snapshot: %w", err)
	}
	if err := writeCgroupValue(s.cgroup.path, "cgroup.freeze", "0"); err != nil {
		_ = os.RemoveAll(destination)
		return cellruntime.SnapshotInfo{}, fmt.Errorf("thaw session after snapshot: %w", err)
	}
	thawed = true
	return cellruntime.SnapshotInfo{ID: snapshotID, Size: int64(size), Flattened: flatten}, nil
}

func (r *Runtime) DeleteSnapshot(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
		return errors.New("invalid native snapshot id")
	}
	if !strings.HasPrefix(id, "snp_") {
		return errors.New("invalid native snapshot id")
	}
	return os.RemoveAll(filepath.Join(r.root, "snapshots", id))
}

func waitCgroupFrozen(ctx context.Context, path string) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
		if err != nil {
			return fmt.Errorf("read cgroup frozen state: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "frozen 1" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for sandbox cgroup to freeze")
		case <-ticker.C:
		}
	}
}

func copyOverlayTree(source, destination string) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	if err := copyOverlayXattrs(source, destination); err != nil {
		return fmt.Errorf("copy overlay root attributes: %w", err)
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(destination, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			err = os.Mkdir(dst, mode.Perm())
		case mode.IsRegular():
			err = copySnapshotFile(path, dst, mode.Perm())
		case mode&os.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(path)
			if err == nil {
				err = os.Symlink(target, dst)
			}
		case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0:
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("read device metadata for %s", rel)
			}
			err = unix.Mknod(dst, st.Mode&unix.S_IFMT, int(st.Rdev))
		case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("read device metadata for %s", rel)
			}
			err = unix.Mknod(dst, st.Mode&unix.S_IFMT, int(st.Rdev))
		default:
			return fmt.Errorf("unsupported overlay entry type %s", rel)
		}
		if err != nil {
			return fmt.Errorf("copy overlay entry %s: %w", rel, err)
		}
		if !mode.IsRegular() && mode&os.ModeSymlink == 0 {
			uid, gid := ownerIDs(info)
			if err = os.Chown(dst, uid, gid); err != nil {
				return fmt.Errorf("preserve owner for %s: %w", rel, err)
			}
			if err = unix.Chmod(dst, fileMode(info)); err != nil {
				return fmt.Errorf("preserve mode for %s: %w", rel, err)
			}
		}
		return copyOverlayXattrs(path, dst)
	})
}

func copyMergedTree(source, destination string) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	return copyMergedDirectory(source, destination, "")
}

func copyMergedDirectory(source, destination, relative string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if relative == "" && (name == "dev" || name == "proc" || name == "sys" || name == "run") {
			continue
		}
		src := filepath.Join(source, name)
		dst := filepath.Join(destination, name)
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			err = os.Mkdir(dst, mode.Perm())
		case mode.IsRegular():
			err = copySnapshotFile(src, dst, mode.Perm())
		case mode&os.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(src)
			if err == nil {
				err = os.Symlink(target, dst)
			}
		case mode&os.ModeDevice != 0:
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("read device metadata for %s", filepath.Join(relative, name))
			}
			err = unix.Mknod(dst, st.Mode&unix.S_IFMT, int(st.Rdev))
		default:
			return fmt.Errorf("unsupported merged filesystem entry %s", filepath.Join(relative, name))
		}
		if err != nil {
			return fmt.Errorf("copy merged filesystem entry %s: %w", filepath.Join(relative, name), err)
		}
		if mode.IsDir() || mode&os.ModeDevice != 0 {
			uid, gid := ownerIDs(info)
			if err := os.Chown(dst, uid, gid); err != nil {
				return fmt.Errorf("preserve merged owner for %s: %w", filepath.Join(relative, name), err)
			}
			if err := unix.Chmod(dst, fileMode(info)); err != nil {
				return fmt.Errorf("preserve merged mode for %s: %w", filepath.Join(relative, name), err)
			}
			if err := copyOverlayXattrs(src, dst); err != nil {
				return fmt.Errorf("preserve merged attributes for %s: %w", filepath.Join(relative, name), err)
			}
		}
		if mode.IsDir() {
			if err := copyMergedDirectory(src, dst, filepath.Join(relative, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func ownerIDs(info os.FileInfo) (int, int) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid)
	}
	return os.Getuid(), os.Getgid()
}

func copySnapshotFile(source, destination string, mode os.FileMode) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if err = unix.IoctlFileClone(int(dst.Fd()), int(src.Fd())); err != nil {
		if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.EOPNOTSUPP) && !errors.Is(err, syscall.ENOTTY) && !errors.Is(err, syscall.EINVAL) {
			_ = dst.Close()
			return err
		}
		if _, err = src.Seek(0, io.SeekStart); err == nil {
			_, err = io.Copy(dst, src)
		}
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(source)
		if err == nil {
			uid, gid := ownerIDs(info)
			err = os.Chown(destination, uid, gid)
			if err == nil {
				err = unix.Chmod(destination, fileMode(info))
			}
		}
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(source)
		if err == nil {
			err = os.Chtimes(destination, info.ModTime(), info.ModTime())
		}
	}
	closeErr := dst.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return copyOverlayXattrs(source, destination)
}

func fileMode(info os.FileInfo) uint32 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Mode & 07777
	}
	return uint32(info.Mode().Perm())
}

func copyOverlayXattrs(source, destination string) error {
	size, err := unix.Llistxattr(source, nil)
	if err != nil {
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
			return nil
		}
		return err
	}
	if size == 0 {
		return nil
	}
	buffer := make([]byte, size)
	read, err := unix.Llistxattr(source, buffer)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(buffer[:read]), "\x00") {
		if name == "" {
			continue
		}
		valueSize, getErr := unix.Lgetxattr(source, name, nil)
		if getErr != nil {
			return getErr
		}
		value := make([]byte, valueSize)
		if _, getErr = unix.Lgetxattr(source, name, value); getErr != nil {
			return getErr
		}
		if setErr := unix.Lsetxattr(destination, name, value, 0); setErr != nil {
			return setErr
		}
	}
	return nil
}
