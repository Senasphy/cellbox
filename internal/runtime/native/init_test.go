//go:build linux

package native

import (
	"context"
	"strings"
	"testing"
)

func validInitConfig() InitConfig {
	return InitConfig{
		SessionID: "session-123",
		LowerDirs: []string{"/var/lib/cellbox/images/rootfs"},
		UpperDir:  "/var/lib/cellbox/sessions/session-123/upper",
		WorkDir:   "/var/lib/cellbox/sessions/session-123/work",
		RootDir:   "/var/lib/cellbox/sessions/session-123/root",
		RunDir:    "/var/lib/cellbox/sessions/session-123/run",
		Hostname:  "session-123",
	}
}

func TestValidateInitConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*InitConfig)
		want   string
	}{
		{name: "valid"},
		{name: "empty id", mutate: func(c *InitConfig) { c.SessionID = "" }, want: "session id"},
		{name: "id path separator", mutate: func(c *InitConfig) { c.SessionID = "../bad" }, want: "session id"},
		{name: "relative lower path", mutate: func(c *InitConfig) { c.LowerDirs[0] = "rootfs" }, want: "clean and absolute"},
		{name: "unclean work path", mutate: func(c *InitConfig) { c.WorkDir = "/tmp/../work" }, want: "clean and absolute"},
		{name: "long hostname", mutate: func(c *InitConfig) { c.Hostname = strings.Repeat("h", 64) }, want: "63 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validInitConfig()
			if test.mutate != nil {
				test.mutate(&config)
			}
			err := validateInitConfig(config)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validate config: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestSpawnInitRequiresCgroupFD(t *testing.T) {
	_, err := SpawnInit(context.Background(), validInitConfig(), -1)
	if err == nil || !strings.Contains(err.Error(), "cgroup directory fd") {
		t.Fatalf("SpawnInit error = %v", err)
	}
}
