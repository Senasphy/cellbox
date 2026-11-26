//go:build linux

package native

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
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
	clean, _, err := sandboxPath(value)
	if err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	if s.init == nil {
		return nil, cellruntime.FileInfo{}, errors.New("native sandbox init process is unavailable")
	}
	return s.init.fsOpenRead(ctx, clean)
}

func (p *InitProcess) fsOpenRead(ctx context.Context, cleanPath string) (io.ReadCloser, cellruntime.FileInfo, error) {
	payload, err := json.Marshal(fsPathRequest{Path: cleanPath})
	if err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	msg, files, err := p.controlCall(ctx, "fs_open_read", "fs", payload)
	if err != nil {
		return nil, cellruntime.FileInfo{}, err
	}
	if msg.Type != "fs_read_ready" || len(files) != 1 {
		closeFiles(files)
		return nil, cellruntime.FileInfo{}, fmt.Errorf("unexpected native fs_open_read response %q with %d descriptors", msg.Type, len(files))
	}
	var info cellruntime.FileInfo
	if err := json.Unmarshal(msg.Payload, &info); err != nil {
		closeFiles(files)
		return nil, cellruntime.FileInfo{}, err
	}
	return files[0], info, nil
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
	if s.init == nil {
		return errors.New("native sandbox init process is unavailable")
	}
	return s.init.fsWrite(ctx, clean, source, mode)
}

func (p *InitProcess) fsWrite(ctx context.Context, cleanPath string, source io.Reader, mode os.FileMode) error {
	openPayload, err := json.Marshal(fsPathRequest{Path: cleanPath})
	if err != nil {
		return err
	}
	msg, files, err := p.controlCall(ctx, "fs_open_write", "fs", openPayload)
	if err != nil {
		return err
	}
	if msg.Type != "fs_write_opened" || len(files) != 1 {
		closeFiles(files)
		return fmt.Errorf("unexpected native fs_open_write response %q with %d descriptors", msg.Type, len(files))
	}
	var opened fsWriteOpenedResponse
	if err := json.Unmarshal(msg.Payload, &opened); err != nil {
		closeFiles(files)
		return err
	}
	file := files[0]
	abort := func() {
		abortPayload, _ := json.Marshal(fsWriteCommitRequest{Path: cleanPath, TempName: opened.TempName})
		_, abortFiles, _ := p.controlCall(context.Background(), "fs_abort_write", "fs", abortPayload)
		closeFiles(abortFiles)
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, maxNativeFileBytes+1))
	if copyErr != nil {
		_ = file.Close()
		abort()
		return fmt.Errorf("write sandbox file %s: %w", cleanPath, copyErr)
	}
	if written > maxNativeFileBytes {
		_ = file.Close()
		abort()
		return fmt.Errorf("write sandbox file %s: file exceeds %d byte transfer limit", cleanPath, maxNativeFileBytes)
	}
	if err := file.Chmod(mode.Perm() & 0777); err != nil {
		_ = file.Close()
		abort()
		return fmt.Errorf("write sandbox file %s: %w", cleanPath, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		abort()
		return fmt.Errorf("write sandbox file %s: %w", cleanPath, err)
	}
	if err := file.Close(); err != nil {
		abort()
		return fmt.Errorf("write sandbox file %s: %w", cleanPath, err)
	}
	commitPayload, err := json.Marshal(fsWriteCommitRequest{Path: cleanPath, TempName: opened.TempName})
	if err != nil {
		abort()
		return err
	}
	commitMsg, commitFiles, err := p.controlCall(ctx, "fs_commit_write", "fs", commitPayload)
	closeFiles(commitFiles)
	if err != nil {
		return err
	}
	if commitMsg.Type != "fs_done" {
		return fmt.Errorf("unexpected native fs_commit_write response %q", commitMsg.Type)
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
	if s.init == nil {
		return errors.New("native sandbox init process is unavailable")
	}
	return s.init.fsDelete(ctx, clean)
}

func (p *InitProcess) fsDelete(ctx context.Context, cleanPath string) error {
	payload, err := json.Marshal(fsPathRequest{Path: cleanPath})
	if err != nil {
		return err
	}
	msg, files, err := p.controlCall(ctx, "fs_delete", "fs", payload)
	closeFiles(files)
	if err != nil {
		return err
	}
	if msg.Type != "fs_done" {
		return fmt.Errorf("unexpected native fs_delete response %q", msg.Type)
	}
	return nil
}

func (s *sandbox) ListDir(ctx context.Context, value string) ([]cellruntime.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, _, err := sandboxPath(value)
	if err != nil {
		return nil, err
	}
	if s.init == nil {
		return nil, errors.New("native sandbox init process is unavailable")
	}
	return s.init.fsList(ctx, clean)
}

func (p *InitProcess) fsList(ctx context.Context, cleanPath string) ([]cellruntime.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(fsPathRequest{Path: cleanPath})
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	id, req, err := p.registerRequest("fs", 4)
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	defer p.unregisterRequest(id, req)
	if err := p.sendRequest(ControlMessage{Type: "fs_list", ID: id, Payload: payload}, pw); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	_ = pw.Close()
	type listResult struct {
		entries []cellruntime.FileInfo
		err     error
	}
	decoded := make(chan listResult, 1)
	go func() {
		defer pr.Close()
		var entries []cellruntime.FileInfo
		decodeErr := json.NewDecoder(pr).Decode(&entries)
		decoded <- listResult{entries: entries, err: decodeErr}
	}()
	select {
	case <-ctx.Done():
		_ = pr.Close()
		return nil, ctx.Err()
	case pkt, ok := <-req.ch:
		if !ok {
			_ = pr.Close()
			return nil, p.initExitError(fmt.Errorf("read native fs_list response: %w", p.lastReaderErr()))
		}
		closeFiles(pkt.files)
		if pkt.msg.Type == "error" {
			_ = pr.Close()
			<-decoded
			return nil, errors.New(pkt.msg.Error)
		}
		if pkt.msg.Type != "fs_done" {
			_ = pr.Close()
			<-decoded
			return nil, fmt.Errorf("unexpected native fs_list response %q", pkt.msg.Type)
		}
		res := <-decoded
		if res.err != nil {
			return nil, fmt.Errorf("decode directory listing %s: %w", cleanPath, res.err)
		}
		return res.entries, nil
	}
}

func (s *sandbox) Archive(ctx context.Context, value string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, _, err := sandboxPath(value)
	if err != nil {
		return nil, err
	}
	if s.init == nil {
		return nil, errors.New("native sandbox init process is unavailable")
	}
	return s.init.fsArchive(ctx, clean)
}

func (p *InitProcess) fsArchive(ctx context.Context, cleanPath string) (io.ReadCloser, error) {
	payload, err := json.Marshal(fsPathRequest{Path: cleanPath})
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	id, req, err := p.registerRequest("fs", 4)
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	if err := p.sendRequest(ControlMessage{Type: "fs_tar_create", ID: id, Payload: payload}, pw); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		p.unregisterRequest(id, req)
		return nil, err
	}
	_ = pw.Close()
	select {
	case <-ctx.Done():
		_ = pr.Close()
		p.unregisterRequest(id, req)
		return nil, ctx.Err()
	case pkt, ok := <-req.ch:
		if !ok {
			_ = pr.Close()
			p.unregisterRequest(id, req)
			return nil, p.initExitError(fmt.Errorf("read native fs_tar_create response: %w", p.lastReaderErr()))
		}
		closeFiles(pkt.files)
		if pkt.msg.Type == "error" {
			_ = pr.Close()
			p.unregisterRequest(id, req)
			return nil, errors.New(pkt.msg.Error)
		}
		if pkt.msg.Type != "fs_tar_started" {
			_ = pr.Close()
			p.unregisterRequest(id, req)
			return nil, fmt.Errorf("unexpected native fs_tar_create response %q", pkt.msg.Type)
		}
	}
	done := make(chan error, 1)
	go func() {
		defer p.unregisterRequest(id, req)
		pkt, ok := <-req.ch
		if !ok {
			done <- p.initExitError(fmt.Errorf("read native fs_tar_create completion: %w", p.lastReaderErr()))
			return
		}
		closeFiles(pkt.files)
		if pkt.msg.Type == "error" {
			done <- errors.New(pkt.msg.Error)
			return
		}
		if pkt.msg.Type != "fs_done" {
			done <- fmt.Errorf("unexpected native fs_tar_create completion %q", pkt.msg.Type)
			return
		}
		done <- nil
	}()
	return &archiveStreamReader{pipe: pr, done: done}, nil
}

type archiveStreamReader struct {
	pipe     *os.File
	done     <-chan error
	once     sync.Once
	finalErr error
}

func (r *archiveStreamReader) waitFinal() error {
	r.once.Do(func() {
		r.finalErr = <-r.done
	})
	return r.finalErr
}

func (r *archiveStreamReader) Read(p []byte) (int, error) {
	n, err := r.pipe.Read(p)
	if errors.Is(err, io.EOF) {
		if finalErr := r.waitFinal(); finalErr != nil {
			return n, finalErr
		}
	}
	return n, err
}

func (r *archiveStreamReader) Close() error {
	err := r.pipe.Close()
	_ = r.waitFinal()
	return err
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
	clean, _, err := sandboxPath(target)
	if err != nil {
		return err
	}
	if s.init == nil {
		return errors.New("native sandbox init process is unavailable")
	}
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
	return s.init.fsExtract(ctx, clean, archive)
}

func (p *InitProcess) fsExtract(ctx context.Context, cleanTarget string, archive *os.File) error {
	payload, err := json.Marshal(fsPathRequest{Path: cleanTarget})
	if err != nil {
		return err
	}
	msg, files, err := p.controlCall(ctx, "fs_tar_extract", "fs", payload, archive)
	closeFiles(files)
	if err != nil {
		return err
	}
	if msg.Type != "fs_done" {
		return fmt.Errorf("unexpected native fs_tar_extract response %q", msg.Type)
	}
	return nil
}

func validateNativeArchive(archive io.Reader) error {
	reader := tar.NewReader(archive)
	var expanded int64
	entries := 0
	symlinks := make(map[string]struct{})
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
		for parent := path.Dir(name); parent != "." && parent != ""; parent = path.Dir(parent) {
			if _, isLink := symlinks[parent]; isLink {
				return fmt.Errorf("archive path %q traverses symlink %q", name, parent)
			}
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxNativeExpandedArchiveBytes-expanded {
				return fmt.Errorf("expanded archive exceeds %d byte limit", maxNativeExpandedArchiveBytes)
			}
			expanded += header.Size
			delete(symlinks, name)
		case tar.TypeDir:
			if _, isLink := symlinks[name]; isLink {
				return fmt.Errorf("archive directory %q collides with symlink", name)
			}
		case tar.TypeSymlink:
			if strings.IndexByte(header.Linkname, 0) >= 0 {
				return errors.New("archive symlink target contains NUL")
			}
			symlinks[name] = struct{}{}
		case tar.TypeLink:
			link, err := safeArchiveEntry(header.Linkname)
			if err != nil {
				return fmt.Errorf("unsafe archive hardlink target: %w", err)
			}
			for parent := path.Dir(link); parent != "." && parent != ""; parent = path.Dir(parent) {
				if _, isLink := symlinks[parent]; isLink {
					return fmt.Errorf("archive hardlink target %q traverses symlink %q", link, parent)
				}
			}
			delete(symlinks, name)
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
