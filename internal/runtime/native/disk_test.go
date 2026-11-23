//go:build linux

package native

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpperDirectoryUsageStopsAtQuota(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte(strings.Repeat("x", 16<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	used, err := upperDirectoryUsage(root, make(chan struct{}), 1)
	if !errors.Is(err, errUpperUsageLimit) || used <= 1 {
		t.Fatalf("usage=%d err=%v; want usage over one byte and quota sentinel", used, err)
	}
}

func TestUpperDirectoryUsageTreatsZeroLimitAsUnbounded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte(strings.Repeat("x", 16<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	used, err := upperDirectoryUsage(root, nil, 0)
	if err != nil || used == 0 {
		t.Fatalf("usage=%d err=%v; want nonzero usage without a limit", used, err)
	}
}
