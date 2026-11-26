// Package runtime defines the backend-independent sandbox contract.
package runtime

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// MaxExecOutputBytes is the largest output budget accepted by the API and runtimes.
const MaxExecOutputBytes = 16 << 20

// ErrUnsupported marks a capability that a backend does not provide.
var ErrUnsupported = errors.New("runtime capability unsupported")

// Capabilities describes optional runtime features.
type Capabilities struct {
	Snapshots           bool `json:"snapshots"`
	TTY                 bool `json:"tty"`
	TTYExec             bool `json:"tty_exec"`
	CPULimits           bool `json:"cpu_limits"`
	DiskLimits          bool `json:"disk_limits"`
	TimeoutKillsSession bool `json:"timeout_kills_session"`
	EgressPolicy        bool `json:"egress_policy"`
	WarmPool            bool `json:"warm_pool"`
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

type EgressRule struct {
	Host  string `json:"host"`
	Ports []int  `json:"ports"`
}

type NetworkPolicy struct {
	Mode                 string       `json:"mode"`
	Allow                []EgressRule `json:"allow,omitempty"`
	ExtraBlockedCIDRs    []string     `json:"-"`
	BandwidthBytesPerSec int64        `json:"-"`
}

type EgressEvent struct {
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Resolved  string    `json:"resolved_ip,omitempty"`
	Verdict   string    `json:"verdict"`
	Reason    string    `json:"reason"`
	BytesUp   int64     `json:"bytes_up"`
	BytesDown int64     `json:"bytes_down"`
	Duration  int64     `json:"duration_ms"`
	Timestamp time.Time `json:"timestamp"`
}

// SandboxSpec contains the inputs needed to create a sandbox.
type SandboxSpec struct {
	ID             string
	Image          ImageInfo
	SnapshotLayers []string
	Limits         Limits
	Env            map[string]string
	NetworkMode    string
	NetworkPolicy  NetworkPolicy
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
	ExitCode     int
	Signal       int
	DurationMS   int64
	OutputBytes  int64
	Truncated    bool
	OOMKilled    bool
	PIDsLimitHit bool
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
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Size      int64  `json:"size"`
	Flattened bool   `json:"flattened,omitempty"`
}

// Stats contains current sandbox resource measurements.
type Stats struct {
	CPUUsageNS    uint64 `json:"cpu_usage_ns"`
	MemoryBytes   uint64 `json:"memory_bytes"`
	PIDs          int    `json:"pids"`
	IOReadBytes   uint64 `json:"io_read_bytes"`
	IOWriteBytes  uint64 `json:"io_write_bytes"`
	OOMKills      uint64 `json:"oom_kills"`
	PIDsMaxEvents uint64 `json:"pids_max_events"`
}

// LimitBreach is an asynchronous session-level resource limit event.
type LimitBreach struct {
	Kind       string    `json:"kind"`
	Counter    uint64    `json:"counter,omitempty"`
	UsedBytes  uint64    `json:"used_bytes,omitempty"`
	LimitBytes uint64    `json:"limit_bytes,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Runtime creates and reconciles sandboxes for one backend.
type Runtime interface {
	Name() string
	Capabilities() Capabilities
	EnsureImage(context.Context, string) (ImageInfo, error)
	Create(context.Context, SandboxSpec) (Sandbox, error)
	Reconcile(context.Context) error
}

// ImageManager is an optional runtime capability for managing cached images.
type ImageManager interface {
	ListImages(context.Context) ([]ImageInfo, error)
	RemoveImage(context.Context, string) error
}

// SnapshotManager removes backend-owned immutable snapshot data.
type SnapshotManager interface {
	DeleteSnapshot(context.Context, string) error
}

// SnapshotFlattener can materialize a session's merged root into a standalone snapshot layer.
type SnapshotFlattener interface {
	SnapshotFlatten(context.Context) (SnapshotInfo, error)
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
