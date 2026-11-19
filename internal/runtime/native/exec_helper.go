//go:build linux

package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxExecHelperRequest = 1 << 20

type execHelperRequest struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// RunExec starts the command helper stage and replaces it with the workload.
// The close-on-exec error descriptor reports precise execve failures to init.
func RunExec(requestFD, errorFD int) error {
	if _, err := unix.FcntlInt(uintptr(errorFD), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("protect exec error descriptor: %w", err))
	}
	requestFile := os.NewFile(uintptr(requestFD), "cellbox-exec-request")
	if requestFile == nil {
		return reportExecHelperError(errorFD, errors.New("missing exec helper request descriptor"))
	}
	data, err := io.ReadAll(io.LimitReader(requestFile, maxExecHelperRequest+1))
	_ = requestFile.Close()
	if err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("read exec helper request: %w", err))
	}
	if len(data) > maxExecHelperRequest {
		return reportExecHelperError(errorFD, errors.New("exec helper request exceeds size limit"))
	}
	var request execHelperRequest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("decode exec helper request: %w", err))
	}
	if len(request.Argv) == 0 || strings.IndexByte(request.Argv[0], 0) >= 0 {
		return reportExecHelperError(errorFD, errors.New("exec helper requires a valid argv"))
	}
	if request.Cwd != "" && (!path.IsAbs(request.Cwd) || path.Clean(request.Cwd) != request.Cwd) {
		return reportExecHelperError(errorFD, errors.New("exec helper cwd must be a clean absolute path"))
	}
	for key, value := range request.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.IndexByte(value, 0) >= 0 {
			return reportExecHelperError(errorFD, fmt.Errorf("invalid workload environment entry %q", key))
		}
	}
	if err := unix.CloseRange(uint(errorFD+1), ^uint(0), 0); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("close exec helper descriptors: %w", err))
	}
	cwd := request.Cwd
	if cwd == "" {
		cwd = "/"
	}
	if err := os.Chdir(cwd); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("change workload directory: %w", err))
	}
	if err := setWorkloadRlimits(); err != nil {
		return reportExecHelperError(errorFD, err)
	}
	if err := clearWorkloadCapabilities(); err != nil {
		return reportExecHelperError(errorFD, err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("set exec no_new_privs: %w", err))
	}
	if err := InstallDefaultSeccomp(); err != nil {
		return reportExecHelperError(errorFD, fmt.Errorf("install exec seccomp filter: %w", err))
	}
	env := mergeWorkloadEnvironment(os.Environ(), request.Env)
	pathValue := os.Getenv("PATH")
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	if pathValue != "" {
		_ = os.Setenv("PATH", pathValue)
	}
	binary := request.Argv[0]
	if !strings.Contains(binary, "/") {
		binary, err = exec.LookPath(binary)
		if err != nil {
			return reportExecHelperError(errorFD, err)
		}
	}
	if err := syscall.Exec(binary, request.Argv, env); err != nil {
		return reportExecHelperError(errorFD, err)
	}
	return nil
}

func mergeWorkloadEnvironment(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env
}

func setWorkloadRlimits() error {
	for _, limit := range []struct {
		resource int
		value    uint64
	}{
		{syscall.RLIMIT_CORE, 0},
		{syscall.RLIMIT_NOFILE, 1024},
		{syscall.RLIMIT_FSIZE, 1 << 30},
	} {
		rlimit := syscall.Rlimit{Cur: limit.value, Max: limit.value}
		if err := syscall.Setrlimit(limit.resource, &rlimit); err != nil {
			return fmt.Errorf("set workload rlimit %d: %w", limit.resource, err)
		}
	}
	return nil
}

func clearWorkloadCapabilities() error {
	header := &unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := []unix.CapUserData{{}, {}}
	if err := unix.Capset(header, &data[0]); err != nil {
		return fmt.Errorf("clear workload capabilities: %w", err)
	}
	check := []unix.CapUserData{{}, {}}
	if err := unix.Capget(header, &check[0]); err != nil {
		return fmt.Errorf("verify workload capabilities: %w", err)
	}
	for _, entry := range check {
		if entry.Effective != 0 || entry.Permitted != 0 || entry.Inheritable != 0 {
			return errors.New("workload helper retained capabilities")
		}
	}
	return nil
}

func reportExecHelperError(fd int, cause error) error {
	_, _ = unix.Write(fd, []byte(cause.Error()))
	return cause
}
