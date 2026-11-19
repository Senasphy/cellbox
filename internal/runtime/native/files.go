//go:build linux

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/sandboxpath"
	"golang.org/x/sys/unix"
)

const maxNativeDirectoryEntries = 100_000

func sandboxPath(value string) (string, string, error) {
	if err := sandboxpath.Validate(value); err != nil {
		return "", "", err
	}
	clean := path.Clean("/" + value)
	relative := strings.TrimPrefix(clean, "/")
	if relative == "" {
		relative = "."
	}
	return clean, relative, nil
}

func (s *sandbox) openRoot() (int, error) {
	if s.init == nil || s.init.cmd == nil || s.init.cmd.Process == nil {
		return -1, errors.New("native sandbox init process is unavailable")
	}
	root := fmt.Sprintf("/proc/%d/root", s.init.cmd.Process.Pid)
	fd, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open native sandbox root: %w", err)
	}
	return fd, nil
}

func openSandboxPath(rootFD int, relative string, flags int, mode uint32) (int, error) {
	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_IN_ROOT | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func (s *sandbox) ReadFile(ctx context.Context, value string) (io.ReadCloser, cellruntime.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	defer unix.Close(rootFD)
	fd, err := openSandboxPath(rootFD, relative, unix.O_RDONLY, 0)
	if err != nil {
		return nil, cellruntime.FileInfo{}, fmt.Errorf("open sandbox file %s: %w", clean, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return nil, cellruntime.FileInfo{}, err
	}
	file := os.NewFile(uintptr(fd), clean)
	mode := nativeFileMode(stat.Mode)
	return file, cellruntime.FileInfo{Path: clean, Size: stat.Size, Mode: mode, IsDir: mode.IsDir()}, nil
}

func nativeFileMode(mode uint32) os.FileMode {
	result := os.FileMode(mode & 07777)
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		result |= os.ModeDir
	case unix.S_IFLNK:
		result |= os.ModeSymlink
	case unix.S_IFCHR:
		result |= os.ModeDevice | os.ModeCharDevice
	case unix.S_IFBLK:
		result |= os.ModeDevice
	case unix.S_IFIFO:
		result |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		result |= os.ModeSocket
	}
	return result
}

func (s *sandbox) WriteFile(ctx context.Context, value string, source io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return err
	}
	if relative == "." {
		return errors.New("cannot write the sandbox root as a file")
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parent := path.Dir(relative)
	if parent == "" {
		parent = "."
	}
	parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open parent directory for %s: %w", clean, err)
	}
	defer unix.Close(parentFD)
	fd, err := openSandboxPath(parentFD, path.Base(relative), unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return fmt.Errorf("create sandbox file %s: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		return fmt.Errorf("write sandbox file %s: %w", clean, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close sandbox file %s: %w", clean, err)
	}
	return nil
}

func (s *sandbox) DeleteFile(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return err
	}
	if relative == "." {
		return errors.New("cannot delete the sandbox root")
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parent := path.Dir(relative)
	if parent == "" {
		parent = "."
	}
	parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open parent directory for %s: %w", clean, err)
	}
	defer unix.Close(parentFD)
	if err := unix.Unlinkat(parentFD, path.Base(relative), 0); err != nil {
		if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
			err = unix.Unlinkat(parentFD, path.Base(relative), unix.AT_REMOVEDIR)
		}
		if err != nil {
			return fmt.Errorf("delete sandbox path %s: %w", clean, err)
		}
	}
	return nil
}

func (s *sandbox) ListDir(ctx context.Context, value string) ([]cellruntime.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return nil, err
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	fd, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open sandbox directory %s: %w", clean, err)
	}
	directory := os.NewFile(uintptr(fd), clean)
	defer directory.Close()
	entries, err := directory.ReadDir(maxNativeDirectoryEntries + 1)
	if err != nil {
		return nil, fmt.Errorf("read sandbox directory %s: %w", clean, err)
	}
	if len(entries) > maxNativeDirectoryEntries {
		return nil, fmt.Errorf("sandbox directory %s exceeds %d entries", clean, maxNativeDirectoryEntries)
	}
	result := make([]cellruntime.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat sandbox entry %s/%s: %w", clean, entry.Name(), err)
		}
		entryPath := path.Join(clean, entry.Name())
		result = append(result, cellruntime.FileInfo{Path: entryPath, Size: info.Size(), Mode: info.Mode(), IsDir: info.IsDir()})
	}
	return result, nil
}
