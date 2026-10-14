package main

import (
	"bytes"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestKeysCreateListAndRevoke(t *testing.T) {
	dir := t.TempDir()
	secret := strings.TrimSpace(runCLI(t, "keys", "create", "test-key", "--data-dir", dir))
	if !strings.HasPrefix(secret, "cbx_") {
		t.Fatalf("unexpected key %q", secret)
	}
	listed := runCLI(t, "keys", "ls", "--data-dir", dir)
	if !strings.Contains(listed, "test-key") || strings.Contains(listed, secret) {
		t.Fatalf("unexpected key listing %q", listed)
	}
	fields := strings.Fields(listed)
	if len(fields) < 1 {
		t.Fatalf("missing key id: %q", listed)
	}
	runCLI(t, "keys", "revoke", fields[0], "--data-dir", dir)
	listed = runCLI(t, "keys", "ls", "--data-dir", dir)
	if !strings.Contains(listed, "revoked") {
		t.Fatalf("key was not revoked: %q", listed)
	}
}
