package store

import (
	"context"
	"time"
)

// Store is the persistence boundary shared by single-node and cluster implementations.
type Store interface {
	Ping(context.Context) error
	Close() error
}

// KeyRecord contains persisted API key metadata and a secret digest.
type KeyRecord struct {
	ID         string
	Name       string
	Prefix     string
	Hash       []byte
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// SessionRecord contains the persisted, backend-neutral session state.
type SessionRecord struct {
	ID                string
	OwnerKeyID        string
	ImageReference    string
	ImageDigest       string
	RuntimeID         string
	State             string
	LimitsJSON        []byte
	NetworkPolicyJSON []byte
	EnvJSON           []byte
	LabelsJSON        []byte
	CreatedAt         time.Time
	LastActivityAt    time.Time
	ExpiresAt         time.Time
	ParentSnapshotID  string
	FailureReason     string
}

// SessionStore persists session lifecycle transitions.
type SessionStore interface {
	CreateSession(context.Context, SessionRecord) error
	UpdateSession(context.Context, SessionRecord) error
	GetSession(context.Context, string) (SessionRecord, error)
	ListSessions(context.Context, string, bool) ([]SessionRecord, error)
}

// ExecRecord contains the persisted command request and final status.
type ExecRecord struct {
	ID          string
	SessionID   string
	ArgvJSON    []byte
	Shell       string
	Cwd         string
	EnvJSON     []byte
	TTY         bool
	State       string
	ExitCode    *int
	Signal      *int
	StartedAt   time.Time
	EndedAt     *time.Time
	OutputBytes int64
	Truncated   bool
}

// ExecOutputRecord is an ordered, resumable output chunk.
type ExecOutputRecord struct {
	ExecID    string
	Offset    int64
	Stream    string
	Data      []byte
	Timestamp time.Time
}

// ListCursor identifies a stable position in a keyset-paginated list.
type ListCursor struct {
	Timestamp time.Time
	ID        string
}

// ExecStore persists command state and append-only output chunks.
type ExecStore interface {
	CreateExec(context.Context, ExecRecord) error
	UpdateExec(context.Context, ExecRecord) error
	GetExec(context.Context, string) (ExecRecord, error)
	ListExecs(context.Context, string) ([]ExecRecord, error)
	AppendExecOutput(context.Context, string, string, []byte, time.Time) (ExecOutputRecord, error)
	ListExecOutput(context.Context, string, int64) ([]ExecOutputRecord, error)
}
