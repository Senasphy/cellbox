// Package runtime defines the backend-independent sandbox contract.
package runtime

import (
	"context"
	"errors"
	"io"
	"io/fs"
)

// ErrUnsupported marks a capability that a backend does not provide.
var ErrUnsupported = errors.New("runtime capability unsupported")

// Capabilities describes optional runtime features.
type Capabilities struct {
	Snapshots    bool `json:"snapshots"`
	TTY          bool `json:"tty"`
	CPULimits    bool `json:"cpu_limits"`
	DiskLimits   bool `json:"disk_limits"`
	EgressPolicy bool `json:"egress_policy"`
	WarmPool     bool `json:"warm_pool"`
}

// Limits describes enforced per-sandbox resource limits.
type Limits struct {
	CPUMillicores  int `json:"cpu_millicores"`
	MemoryMB       int `json:"memory_mb"`
	PIDs           int `json:"pids"`
	DiskMB         int `json:"disk_mb"`
	TTLSeconds     int `json:"ttl_s"`
	IdleTTLSeconds int `json:"idle_ttl_s"`
}

// ImageInfo identifies a resolved container image.
type ImageInfo struct {
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// SandboxSpec contains the inputs needed to create a sandbox.
type SandboxSpec struct {
	ID          string
	Image       ImageInfo
	Limits      Limits
	Env         map[string]string
	NetworkMode string
}

// ExecRequest describes one workload process.
type ExecRequest struct {
	Argv        []string
	Shell       string
	Cwd         string
	Env         map[string]string
	TTY         bool
	TimeoutMS   int
	OutputLimit int64
}

// OutputChunk is one ordered output event.
type OutputChunk struct {
	Stream string
	Data   []byte
}

// ExecResult is the finalized result of a workload process.
type ExecResult struct {
	ExitCode    int
	Signal      int
	DurationMS  int64
	OutputBytes int64
	Truncated   bool
}

// ExecHandle exposes process output and completion.
type ExecHandle interface {
	Output() <-chan OutputChunk
	Wait(context.Context) (ExecResult, error)
	Signal(context.Context, int) error
}

// TerminalRequest describes an interactive terminal.
type TerminalRequest struct {
	Cwd     string
	Env     map[string]string
	Columns uint16
	Rows    uint16
}

// Terminal exposes a PTY stream.
type Terminal interface {
	io.ReadWriteCloser
	Resize(context.Context, uint16, uint16) error
}

// FileInfo describes a sandbox file.
type FileInfo struct {
	Path  string      `json:"path"`
	Size  int64       `json:"size"`
	Mode  fs.FileMode `json:"mode"`
	IsDir bool        `json:"is_dir"`
}

// SnapshotInfo describes an immutable filesystem snapshot.
type SnapshotInfo struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Size     int64  `json:"size"`
}

// Stats contains current sandbox resource measurements.
type Stats struct {
	CPUUsageNS   uint64 `json:"cpu_usage_ns"`
	MemoryBytes  uint64 `json:"memory_bytes"`
	PIDs         int    `json:"pids"`
	IOReadBytes  uint64 `json:"io_read_bytes"`
	IOWriteBytes uint64 `json:"io_write_bytes"`
}

// Runtime creates and reconciles sandboxes for one backend.
type Runtime interface {
	Name() string
	Capabilities() Capabilities
	EnsureImage(context.Context, string) (ImageInfo, error)
	Create(context.Context, SandboxSpec) (Sandbox, error)
	Reconcile(context.Context) error
}

// Sandbox provides process, filesystem, lifecycle and resource operations.
type Sandbox interface {
	ID() string
	Exec(context.Context, ExecRequest) (ExecHandle, error)
	OpenTerminal(context.Context, TerminalRequest) (Terminal, error)
	ReadFile(context.Context, string) (io.ReadCloser, FileInfo, error)
	WriteFile(context.Context, string, io.Reader, fs.FileMode) error
	DeleteFile(context.Context, string) error
	ListDir(context.Context, string) ([]FileInfo, error)
	Archive(context.Context, string) (io.ReadCloser, error)
	Extract(context.Context, string, io.Reader) error
	Snapshot(context.Context) (SnapshotInfo, error)
	UpdateLimits(context.Context, Limits) error
	Stats(context.Context) (Stats, error)
	Kill(context.Context) error
	Destroy(context.Context) error
}
