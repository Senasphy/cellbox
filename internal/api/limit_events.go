package api

import (
	"context"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type limitBreachRecorder interface {
	RecordLimitBreach(context.Context, string, string, string, any, time.Time) error
}

func (s *server) watchLimitEvents(sessionID, keyID string, events <-chan runtime.LimitBreach) {
	for event := range events {
		at := event.Timestamp
		if at.IsZero() {
			at = time.Now().UTC()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		failureReason := "disk limit exceeded; native session was stopped"
		if event.Kind == "memory" {
			failureReason = "native cgroup reported an out-of-memory kill; session was stopped"
		} else if event.Kind == "pids" {
			failureReason = "native cgroup rejected process creation at the PID limit; session was stopped"
		} else if event.Reason == "measurement_failed" {
			failureReason = "disk usage could not be measured safely; native session was stopped"
		}
		if recorder, ok := s.store.(limitBreachRecorder); ok {
			detail := map[string]any{"kind": event.Kind, "used_bytes": event.UsedBytes, "limit_bytes": event.LimitBytes, "counter": event.Counter, "reason": event.Reason}
			_ = recorder.RecordLimitBreach(ctx, sessionID, keyID, failureReason, detail, at)
		}
		cancel()
	}
}

func (s *server) watchEgressEvents(sessionID, keyID string, events <-chan runtime.EgressEvent) {
	recorder, ok := s.store.(store.EgressLogRecorder)
	for event := range events {
		if !ok {
			continue
		}
		at := event.Timestamp
		if at.IsZero() {
			at = time.Now().UTC()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = recorder.RecordEgressLog(ctx, sessionID, keyID, store.EgressLogEntry{
			Timestamp: at, Host: event.Host, Port: event.Port, Resolved: event.Resolved,
			Verdict: event.Verdict, Reason: event.Reason, BytesUp: event.BytesUp,
			BytesDown: event.BytesDown, Duration: event.Duration,
		})
		cancel()
	}
}
