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
	"strings"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sys/unix"
)

type fsPathRequest struct {
	Path string `json:"path"`
}

type fsWriteCommitRequest struct {
	Path     string `json:"path"`
	TempName string `json:"temp_name"`
}

type fsWriteOpenedResponse struct {
	TempName string `json:"temp_name"`
}

func validUploadTempName(name string) bool {
	return strings.HasPrefix(name, ".cellbox-upload-") && !strings.ContainsAny(name, `/\`) && name != "." && name != ".."
}

func openInitRoot(rootPath string) (int, error) {
	if rootPath == "" {
		rootPath = "/"
	}
	fd, err := unix.Open(rootPath, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open sandbox root: %w", err)
	}
	return fd, nil
}

func listSandboxDir(rootPath, value string) ([]cellruntime.FileInfo, error) {
	clean, relative, err := sandboxPath(value)
	if err != nil {
		return nil, err
	}
	rootFD, err := openInitRoot(rootPath)
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

func handleFSRequest(rootPath string, message ControlMessage, files []*os.File, send func(ControlMessage) error, sendFiles func(ControlMessage, ...*os.File) error) {
	switch message.Type {
	case "fs_open_read":
		closeFiles(files)
		go func() {
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			fd, stat, err := openSandboxRegularRead(rootFD, relative)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open sandbox file %s: %v", clean, err)})
				return
			}
			file := os.NewFile(uintptr(fd), clean)
			defer file.Close()
			mode := nativeFileMode(stat.Mode)
			infoPayload, err := json.Marshal(cellruntime.FileInfo{Path: clean, Size: stat.Size, Mode: mode, IsDir: mode.IsDir()})
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			_ = sendFiles(ControlMessage{Type: "fs_read_ready", ID: message.ID, Payload: infoPayload}, file)
		}()
	case "fs_open_write":
		closeFiles(files)
		go func() {
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if relative == "." {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "cannot write the sandbox root as a file"})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			parent := path.Dir(relative)
			if parent == "" {
				parent = "."
			}
			parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open parent directory for %s: %v", clean, err)})
				return
			}
			defer unix.Close(parentFD)
			var tempName string
			fd := -1
			for attempts := 0; attempts < 8; attempts++ {
				var random [16]byte
				if _, randErr := rand.Read(random[:]); randErr != nil {
					_ = send(ControlMessage{Type: "error", ID: message.ID, Error: randErr.Error()})
					return
				}
				tempName = ".cellbox-upload-" + hex.EncodeToString(random[:])
				fd, err = unix.Openat(parentFD, tempName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
				if err == nil {
					break
				}
				if !errors.Is(err, unix.EEXIST) {
					_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("write sandbox file %s: %v", clean, err)})
					return
				}
			}
			if fd < 0 {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "could not allocate a temporary upload file"})
				return
			}
			file := os.NewFile(uintptr(fd), tempName)
			payload, _ := json.Marshal(fsWriteOpenedResponse{TempName: tempName})
			if err := sendFiles(ControlMessage{Type: "fs_write_opened", ID: message.ID, Payload: payload}, file); err != nil {
				_ = file.Close()
				_ = unix.Unlinkat(parentFD, tempName, 0)
				return
			}
			_ = file.Close()
		}()
	case "fs_commit_write":
		closeFiles(files)
		go func() {
			var req fsWriteCommitRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if !validUploadTempName(req.TempName) {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "invalid temporary upload file name"})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if relative == "." {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "cannot write the sandbox root as a file"})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			parent := path.Dir(relative)
			if parent == "" {
				parent = "."
			}
			parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open parent directory for %s: %v", clean, err)})
				return
			}
			defer unix.Close(parentFD)
			if err := unix.Renameat(parentFD, req.TempName, parentFD, path.Base(relative)); err != nil {
				_ = unix.Unlinkat(parentFD, req.TempName, 0)
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("write sandbox file %s: %v", clean, err)})
				return
			}
			dirFD, err := unix.Openat(parentFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err == nil {
				err = unix.Fsync(dirFD)
				_ = unix.Close(dirFD)
			}
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("write sandbox file %s: %v", clean, err)})
				return
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	case "fs_abort_write":
		closeFiles(files)
		go func() {
			var req fsWriteCommitRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if !validUploadTempName(req.TempName) {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "invalid temporary upload file name"})
				return
			}
			_, relative, err := sandboxPath(req.Path)
			if err == nil && relative != "." {
				if rootFD, rootErr := openInitRoot(rootPath); rootErr == nil {
					parent := path.Dir(relative)
					if parent == "" {
						parent = "."
					}
					if parentFD, parentErr := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0); parentErr == nil {
						_ = unix.Unlinkat(parentFD, req.TempName, 0)
						_ = unix.Close(parentFD)
					}
					_ = unix.Close(rootFD)
				}
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	case "fs_delete":
		closeFiles(files)
		go func() {
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if relative == "." {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "cannot delete the sandbox root"})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			parent := path.Dir(relative)
			if parent == "" {
				parent = "."
			}
			parentFD, err := openSandboxPath(rootFD, parent, unix.O_PATH|unix.O_DIRECTORY, 0)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open parent directory for %s: %v", clean, err)})
				return
			}
			defer unix.Close(parentFD)
			if err := unix.Unlinkat(parentFD, path.Base(relative), 0); err != nil {
				if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
					err = unix.Unlinkat(parentFD, path.Base(relative), unix.AT_REMOVEDIR)
				}
				if err != nil {
					_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("delete sandbox path %s: %v", clean, err)})
					return
				}
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	case "fs_list":
		if len(files) != 1 {
			closeFiles(files)
			_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "fs_list requires one pipe descriptor"})
			return
		}
		pipeWriter := files[0]
		go func() {
			defer pipeWriter.Close()
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			entries, err := listSandboxDir(rootPath, req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if err := json.NewEncoder(pipeWriter).Encode(entries); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	case "fs_tar_create":
		if len(files) != 1 {
			closeFiles(files)
			_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "fs_tar_create requires one pipe descriptor"})
			return
		}
		pipeWriter := files[0]
		go func() {
			defer pipeWriter.Close()
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			fd, err := openSandboxPath(rootFD, relative, unix.O_PATH|unix.O_NOFOLLOW, 0)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open sandbox archive path %s: %v", clean, err)})
				return
			}
			var stat unix.Stat_t
			err = unix.Fstat(fd, &stat)
			_ = unix.Close(fd)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if err := send(ControlMessage{Type: "fs_tar_started", ID: message.ID}); err != nil {
				return
			}
			tarWriter := tar.NewWriter(pipeWriter)
			var archived int64
			var archiveErr error
			if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
				archiveErr = archiveDirectory(context.Background(), rootFD, relative, "", tarWriter, &archived)
			} else {
				name := path.Base(clean)
				if clean == "/" {
					name = "."
				}
				archiveErr = archiveEntry(context.Background(), rootFD, relative, name, tarWriter, &archived)
			}
			if closeErr := tarWriter.Close(); archiveErr == nil {
				archiveErr = closeErr
			}
			if archiveErr != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: archiveErr.Error()})
				return
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	case "fs_tar_extract":
		if len(files) != 1 {
			closeFiles(files)
			_ = send(ControlMessage{Type: "error", ID: message.ID, Error: "fs_tar_extract requires one archive descriptor"})
			return
		}
		archiveFile := files[0]
		go func() {
			defer archiveFile.Close()
			var req fsPathRequest
			if err := json.Unmarshal(message.Payload, &req); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			clean, relative, err := sandboxPath(req.Path)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			if _, err := archiveFile.Seek(0, io.SeekStart); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			rootFD, err := openInitRoot(rootPath)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			defer unix.Close(rootFD)
			destinationFD, err := openSandboxPath(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: fmt.Sprintf("open extraction directory %s: %v", clean, err)})
				return
			}
			defer unix.Close(destinationFD)
			if err := extractNativeArchive(context.Background(), destinationFD, archiveFile); err != nil {
				_ = send(ControlMessage{Type: "error", ID: message.ID, Error: err.Error()})
				return
			}
			_ = send(ControlMessage{Type: "fs_done", ID: message.ID})
		}()
	}
}
