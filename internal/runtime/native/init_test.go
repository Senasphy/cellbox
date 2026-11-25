//go:build linux

package native

import (
	"context"
	"os"
	"path/filepath"
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

func TestRemoveTreeHandlesRestrictivePermissionsAndReconcileSweepsOrphanDirs(t *testing.T) {
	root := t.TempDir()
	lockedDir := filepath.Join(root, "locked")
	if err := os.MkdirAll(lockedDir, 0700); err != nil {
		t.Fatal(err)
	}
	lockedFile := filepath.Join(lockedDir, "secret.txt")
	if err := os.WriteFile(lockedFile, []byte("x"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedFile, 0000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedDir, 0500); err != nil {
		t.Fatal(err)
	}
	if err := removeTree(lockedDir); err != nil {
		t.Fatalf("removeTree: %v", err)
	}
	if _, err := os.Stat(lockedDir); !os.IsNotExist(err) {
		t.Fatalf("locked directory still exists: %v", err)
	}

	dataDir := filepath.Join(root, "data")
	cgroupDir := filepath.Join(root, "cgroups")
	if err := os.MkdirAll(cgroupDir, 0750); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(dataDir, &CgroupRoot{path: cgroupDir})
	if err != nil {
		t.Fatal(err)
	}
	orphanDir := filepath.Join(dataDir, "sessions", "ses_orphan_dir_only")
	if err := os.MkdirAll(filepath.Join(orphanDir, "upper"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Fatalf("orphan session directory still exists after Reconcile: %v", err)
	}
}
