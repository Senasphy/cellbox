package api

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/sandboxpath"
)

const maxFileTransferBytes = 256 << 20
const maxArchiveTransferBytes = 512 << 20

func (s *server) readFile(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	reader, info, err := box.ReadFile(r.Context(), target)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer reader.Close()
	if info.IsDir {
		problem.Write(w, problem.Problem{Title: "Path is a directory", Status: http.StatusUnprocessableEntity, Code: "path_not_file"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(target)}))
	w.Header().Set("X-Cellbox-File-Mode", fmt.Sprintf("%04o", info.Mode.Perm()))
	w.WriteHeader(http.StatusOK)
	if _, err = io.Copy(w, io.LimitReader(reader, maxFileTransferBytes)); err != nil {
		return
	}
}

func (s *server) writeFile(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	mode := uint64(0644)
	if raw := r.Header.Get("X-Cellbox-Mode"); raw != "" {
		v, err := strconv.ParseUint(raw, 8, 12)
		if err != nil || v > 0777 {
			problem.Write(w, problem.Problem{Title: "Invalid file mode", Status: http.StatusUnprocessableEntity, Code: "file_mode_invalid"})
			return
		}
		mode = v
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferBytes)
	err := box.WriteFile(r.Context(), target, r.Body, fs.FileMode(mode))
	if err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteFile(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	if target == "/" {
		problem.Write(w, problem.Problem{Title: "Invalid path", Status: http.StatusUnprocessableEntity, Code: "path_invalid"})
		return
	}
	if err := box.DeleteFile(r.Context(), target); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listFiles(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	limit, ok := parseListLimit(w, r)
	if !ok {
		return
	}
	cursor, ok := parseFileCursor(w, r)
	if !ok {
		return
	}
	items, err := box.ListDir(r.Context(), target)
	if err != nil {
		writeFileError(w, err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	start := 0
	if cursor != "" {
		start = sort.Search(len(items), func(i int) bool { return items[i].Path > cursor })
	}
	items = items[start:]
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	response := map[string]any{"items": items}
	if hasMore && len(items) > 0 {
		response["next_cursor"] = encodeFileCursor(items[len(items)-1].Path)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) downloadArchive(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	reader, err := box.Archive(r.Context(), target)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(target) + ".tar"}))
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(reader, maxArchiveTransferBytes))
}

func (s *server) uploadArchive(w http.ResponseWriter, r *http.Request) {
	box, ok := s.fileSandbox(w, r)
	if !ok {
		return
	}
	target, ok := fileTarget(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxArchiveTransferBytes)
	if err := box.Extract(r.Context(), target, r.Body); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) fileSandbox(w http.ResponseWriter, r *http.Request) (runtime.Sandbox, bool) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return nil, false
	}
	if session.State != "ready" {
		problem.Write(w, problem.Problem{Title: "Session not ready", Status: http.StatusConflict, Code: "session_not_ready"})
		return nil, false
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || session.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return nil, false
	}
	box := resolver.SandboxByID(session.RuntimeID)
	if box == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the runtime session is no longer available"})
		return nil, false
	}
	return box, true
}

func fileTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	target := r.URL.Query().Get("path")
	if target == "" {
		target = "/"
	}
	if err := sandboxpath.Validate(target); err != nil {
		code, title := "path_invalid", "Invalid path"
		if errors.Is(err, sandboxpath.ErrEscape) {
			code, title = "path_outside_sandbox", "Path outside sandbox"
		}
		problem.Write(w, problem.Problem{Title: title, Status: http.StatusUnprocessableEntity, Code: code, Detail: err.Error()})
		return "", false
	}
	clean := path.Clean("/" + target)
	return clean, true
}

func writeFileError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	code := "file_operation_failed"
	if strings.Contains(err.Error(), "returned 404") {
		status = http.StatusNotFound
		code = "path_not_found"
	} else if strings.Contains(err.Error(), "exceeds") || strings.Contains(err.Error(), "archive path") || strings.Contains(err.Error(), "escapes") {
		status = http.StatusUnprocessableEntity
		code = "file_transfer_invalid"
	} else if strings.Contains(err.Error(), "request body too large") {
		status = http.StatusRequestEntityTooLarge
		code = "file_too_large"
	}
	problem.Write(w, problem.Problem{Title: "File operation failed", Status: status, Code: code, Detail: err.Error()})
}
