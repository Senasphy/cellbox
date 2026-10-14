package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New builds a slog logger and redacts attributes with secret-like names.
func New(level, format string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: l, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		n := strings.ToLower(strings.ReplaceAll(a.Key, "-", "_"))
		for _, s := range []string{"secret", "token", "password", "authorization", "api_key", "private_key"} {
			if strings.Contains(n, s) {
				return slog.String(a.Key, "[REDACTED]")
			}
		}
		return a
	}}
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
