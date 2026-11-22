//go:build linux

package native

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/sandboxpath"
	"golang.org/x/sys/unix"
)

const maxNativeDirectoryEntries = 100_000
const maxNativeFileBytes = int64(256 << 20)
const maxNativeArchiveBytes = int64(512 << 20)
const maxNativeExpandedArchiveBytes = int64(20 << 30)

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
	fd, stat, err := openSandboxRegularRead(rootFD, relative)
	if err != nil {
		return nil, cellruntime.FileInfo{}, fmt.Errorf("open sandbox file %s: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	mode := nativeFileMode(stat.Mode)
	return file, cellruntime.FileInfo{Path: clean, Size: stat.Size, Mode: mode, IsDir: mode.IsDir()}, nil
}

func openSandboxRegularRead(rootFD int, relative string) (int, unix.Stat_t, error) {
	fd, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("sandbox path is not a regular file")
	}
	if stat.Size < 0 || stat.Size > maxNativeFileBytes {
		unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("file exceeds %d byte transfer limit", maxNativeFileBytes)
	}
	return fd, stat, nil
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
	if err := writeSandboxFileAtomic(parentFD, path.Base(relative), source, mode); err != nil {
		return fmt.Errorf("write sandbox file %s: %w", clean, err)
	}
	return nil
}

func writeSandboxFileAtomic(parentFD int, name string, source io.Reader, mode os.FileMode) error {
	var tempName string
	var fd int
	var err error
	for attempts := 0; attempts < 8; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		tempName = ".cellbox-upload-" + hex.EncodeToString(random[:])
		fd, err = unix.Openat(parentFD, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EEXIST) {
			return err
		}
	}
	if fd < 0 {
		return errors.New("could not allocate a temporary upload file")
	}
	keep := false
	defer func() {
		if !keep {
			_ = unix.Unlinkat(parentFD, tempName, 0)
		}
	}()
	file := os.NewFile(uintptr(fd), tempName)
	written, copyErr := io.Copy(file, io.LimitReader(source, maxNativeFileBytes+1))
	if copyErr != nil {
		_ = file.Close()
		return copyErr
	}
	if written > maxNativeFileBytes {
		_ = file.Close()
		return fmt.Errorf("file exceeds %d byte transfer limit", maxNativeFileBytes)
	}
	if err := file.Chmod(mode.Perm() & 0777); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, tempName, parentFD, name); err != nil {
		return err
	}
	dirFD, err := unix.Openat(parentFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	syncErr := unix.Fsync(dirFD)
	unix.Close(dirFD)
	if syncErr != nil {
		return syncErr
	}
	keep = true
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
		var stat unix.Stat_t
		if err := unix.Fstatat(fd, entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, fmt.Errorf("stat sandbox entry %s/%s: %w", clean, entry.Name(), err)
		}
		entryPath := path.Join(clean, entry.Name())
		mode := nativeFileMode(stat.Mode)
		result = append(result, cellruntime.FileInfo{Path: entryPath, Size: stat.Size, Mode: mode, IsDir: mode.IsDir()})
	}
	return result, nil
}

func (s *sandbox) Archive(ctx context.Context, value string) (io.ReadCloser, error) {
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return nil, err
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return nil, err
	}
	fd, err := openSandboxPath(rootFD, relative, unix.O_PATH|unix.O_NOFOLLOW, 0)
	if err != nil {
		unix.Close(rootFD)
		return nil, fmt.Errorf("open sandbox archive path %s: %w", clean, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		unix.Close(rootFD)
		return nil, err
	}
	unix.Close(fd)
	reader, writer := io.Pipe()
	go func() {
		defer unix.Close(rootFD)
		tarWriter := tar.NewWriter(writer)
		var archived int64
		var archiveErr error
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			archiveErr = archiveDirectory(ctx, rootFD, relative, "", tarWriter, &archived)
		} else {
			name := path.Base(clean)
			if clean == "/" {
				name = "."
			}
			archiveErr = archiveEntry(ctx, rootFD, relative, name, tarWriter, &archived)
		}
		if closeErr := tarWriter.Close(); archiveErr == nil {
			archiveErr = closeErr
		}
		_ = writer.CloseWithError(archiveErr)
	}()
	return reader, nil
}

func archiveDirectory(ctx context.Context, rootFD int, relative, prefix string, writer *tar.Writer, total *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open sandbox directory for archive: %w", err)
	}
	directory := os.NewFile(uintptr(fd), relative)
	defer directory.Close()
	entries, err := directory.ReadDir(maxNativeDirectoryEntries + 1)
	if err != nil {
		return err
	}
	if len(entries) > maxNativeDirectoryEntries {
		return fmt.Errorf("archive directory exceeds %d entries", maxNativeDirectoryEntries)
	}
	for _, entry := range entries {
		child := entry.Name()
		childRel := path.Join(relative, child)
		childName := child
		if prefix != "" {
			childName = path.Join(prefix, child)
		}
		if err := archiveEntry(ctx, rootFD, childRel, childName, writer, total); err != nil {
			return err
		}
	}
	return nil
}

func archiveEntry(ctx context.Context, rootFD int, relative, name string, writer *tar.Writer, total *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := openSandboxPath(rootFD, relative, unix.O_PATH|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open sandbox archive entry %s: %w", name, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return err
	}
	mode := stat.Mode & unix.S_IFMT
	header := &tar.Header{Name: name, Mode: int64(nativeFileMode(stat.Mode).Perm()), ModTime: time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec)}
	switch mode {
	case unix.S_IFDIR:
		header.Typeflag = tar.TypeDir
		header.Name = strings.TrimSuffix(name, "/") + "/"
		unix.Close(fd)
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		return archiveDirectory(ctx, rootFD, relative, strings.TrimSuffix(name, "/"), writer, total)
	case unix.S_IFLNK:
		unix.Close(fd)
		linkname, err := readSandboxLink(rootFD, relative)
		if err != nil {
			return err
		}
		header.Typeflag = tar.TypeSymlink
		header.Linkname = linkname
		return writer.WriteHeader(header)
	case unix.S_IFREG:
		unix.Close(fd)
		if stat.Size < 0 || stat.Size > maxNativeArchiveBytes-*total {
			return fmt.Errorf("archive exceeds %d byte file-data limit", maxNativeArchiveBytes)
		}
		fd, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open sandbox archive file %s: %w", name, err)
		}
		file := os.NewFile(uintptr(fd), name)
		header.Typeflag = tar.TypeReg
		header.Size = stat.Size
		if err := writer.WriteHeader(header); err != nil {
			file.Close()
			return err
		}
		written, copyErr := io.CopyN(writer, file, stat.Size)
		closeErr := file.Close()
		*total += written
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	default:
		unix.Close(fd)
		// Skip sockets, FIFOs and device nodes.
		return nil
	}
}

func readSandboxLink(rootFD int, relative string) (string, error) {
	parent := path.Dir(relative)
	if parent == "" {
		parent = "."
	}
	parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(parentFD)
	buffer := make([]byte, 64<<10)
	n, err := unix.Readlinkat(parentFD, path.Base(relative), buffer)
	if err != nil {
		return "", err
	}
	if n == len(buffer) {
		return "", errors.New("sandbox symlink target exceeds 64 KiB")
	}
	return string(buffer[:n]), nil
}

func (s *sandbox) Extract(ctx context.Context, target string, source io.Reader) error {
	clean, relative, err := sandboxPath(target)
	if err != nil {
		return err
	}
	rootFD, err := s.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	destinationFD, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open extraction directory %s: %w", clean, err)
	}
	defer unix.Close(destinationFD)
	archive, err := os.CreateTemp("", "cellbox-native-archive-*")
	if err != nil {
		return err
	}
	defer func() { archive.Close(); os.Remove(archive.Name()) }()
	written, err := io.Copy(archive, io.LimitReader(source, maxNativeArchiveBytes+1))
	if err != nil {
		return fmt.Errorf("stage sandbox archive: %w", err)
	}
	if written > maxNativeArchiveBytes {
		return fmt.Errorf("archive exceeds %d byte limit", maxNativeArchiveBytes)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := validateNativeArchive(archive); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return extractNativeArchive(ctx, destinationFD, archive)
}

func validateNativeArchive(archive io.Reader) error {
	reader := tar.NewReader(archive)
	var expanded int64
	entries := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive header: %w", err)
		}
		entries++
		if entries > maxNativeDirectoryEntries*10 {
			return errors.New("archive contains too many entries")
		}
		name, err := safeArchiveEntry(header.Name)
		if err != nil {
			return err
		}
		if name == "." && header.Typeflag != tar.TypeDir {
			return errors.New("archive cannot replace the extraction root")
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxNativeExpandedArchiveBytes-expanded {
				return fmt.Errorf("expanded archive exceeds %d byte limit", maxNativeExpandedArchiveBytes)
			}
			expanded += header.Size
		case tar.TypeSymlink:
			if strings.IndexByte(header.Linkname, 0) >= 0 {
				return errors.New("archive symlink target contains NUL")
			}
		case tar.TypeLink:
			if _, err := safeArchiveEntry(header.Linkname); err != nil {
				return fmt.Errorf("unsafe archive hardlink target: %w", err)
			}
		}
	}
}

func safeArchiveEntry(value string) (string, error) {
	if value == "" || strings.IndexByte(value, 0) >= 0 || path.IsAbs(value) || strings.HasPrefix(value, "\\") {
		return "", fmt.Errorf("unsafe archive path %q", value)
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
		return "", fmt.Errorf("unsafe archive path %q", value)
	}
	return clean, nil
}

func extractNativeArchive(ctx context.Context, destinationFD int, archive io.Reader) error {
	reader := tar.NewReader(archive)
	directoryModes := make(map[string]os.FileMode)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		name, err := safeArchiveEntry(header.Name)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}
		mode := os.FileMode(header.Mode) & 0777
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureArchiveDirectory(destinationFD, name); err != nil {
				return err
			}
			directoryModes[name] = mode
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(destinationFD, name, reader, header.Size, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := createArchiveSymlink(destinationFD, name, header.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			link, err := safeArchiveEntry(header.Linkname)
			if err != nil {
				return err
			}
			if err := createArchiveHardlink(destinationFD, link, name); err != nil {
				return err
			}
		}
	}
	return restoreArchiveDirectoryModes(destinationFD, directoryModes)
}

func ensureArchiveDirectory(rootFD int, relative string) error {
	parentFD, name, err := openSafeParent(rootFD, relative, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := unix.Mkdirat(parentFD, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return fmt.Errorf("archive directory path traverses a symlink: %w", err)
	}
	return unix.Close(fd)
}

func openSafeParent(rootFD int, relative string, create bool) (int, string, error) {
	parent := path.Dir(relative)
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, "", err
	}
	if parent == "." {
		return current, path.Base(relative), nil
	}
	for _, part := range strings.Split(parent, "/") {
		next, openErr := unix.Openat2(current, part, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(current, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(current)
				return -1, "", mkdirErr
			}
			next, openErr = unix.Openat2(current, part, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		}
		if openErr != nil {
			unix.Close(current)
			return -1, "", fmt.Errorf("archive parent path is unsafe: %w", openErr)
		}
		unix.Close(current)
		current = next
	}
	return current, path.Base(relative), nil
}

func removeArchiveEntry(parentFD int, name string) error {
	if err := unix.Unlinkat(parentFD, name, 0); err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	} else if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
		if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		return nil
	} else {
		return err
	}
}

func writeArchiveFile(rootFD int, relative string, source io.Reader, size int64, mode os.FileMode) error {
	parentFD, name, err := openSafeParent(rootFD, relative, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := removeArchiveEntry(parentFD, name); err != nil {
		return err
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{Flags: unix.O_CREAT | unix.O_EXCL | unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: uint64(mode.Perm()), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), relative)
	_, copyErr := io.CopyN(file, source, size)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func createArchiveSymlink(rootFD int, relative, linkname string) error {
	parentFD, name, err := openSafeParent(rootFD, relative, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := removeArchiveEntry(parentFD, name); err != nil {
		return err
	}
	if err := unix.Symlinkat(linkname, parentFD, name); err != nil {
		return err
	}
	return nil
}

func createArchiveHardlink(rootFD int, source, target string) error {
	sourceParent, sourceName, err := openSafeParent(rootFD, source, false)
	if err != nil {
		return err
	}
	defer unix.Close(sourceParent)
	sourceFD, err := unix.Openat2(sourceParent, sourceName, &unix.OpenHow{Flags: unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	err = unix.Fstat(sourceFD, &stat)
	unix.Close(sourceFD)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("archive hardlink target must be a regular file")
	}
	targetParent, targetName, err := openSafeParent(rootFD, target, true)
	if err != nil {
		return err
	}
	defer unix.Close(targetParent)
	if err := removeArchiveEntry(targetParent, targetName); err != nil {
		return err
	}
	return unix.Linkat(sourceParent, sourceName, targetParent, targetName, 0)
}

func restoreArchiveDirectoryModes(rootFD int, modes map[string]os.FileMode) error {
	names := make([]string, 0, len(modes))
	for name := range modes {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.Count(names[i], "/") > strings.Count(names[j], "/") })
	for _, name := range names {
		fd, err := openSandboxPath(rootFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		err = unix.Fchmod(fd, uint32(modes[name].Perm()))
		unix.Close(fd)
		if err != nil {
			return err
		}
	}
	return nil
}
