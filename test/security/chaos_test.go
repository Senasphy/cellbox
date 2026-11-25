//go:build linux && security

package security_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senasphy/cellbox/internal/api"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/reconcile"
	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	nativeruntime "github.com/senasphy/cellbox/internal/runtime/native"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

func TestNativeChaosKillAndReconcileLeavesNoLeaks(t *testing.T) {
	backend, cgroups, cgroupPath, dataDir, image := sharedNativeEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sessionsCgroupDir := filepath.Join(cgroupPath, "sessions")
	sessionsDataDir := filepath.Join(dataDir, "sessions")

	t.Run("kill_mid_exec_and_restrictive_tree_destroy", func(t *testing.T) {
		box, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          fmt.Sprintf("ses_chaos_kill_%d", time.Now().UnixNano()),
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 32},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		res, out := execInBox(t, ctx, box, cellruntime.ExecRequest{
			Argv: []string{"/bin/sh", "-c", "mkdir -p /tmp/locked && printf x >/tmp/locked/file && chmod 0000 /tmp/locked/file /tmp/locked"},
		})
		if res.ExitCode != 0 {
			t.Fatalf("setup restrictive tree failed: %+v %q", res, out)
		}
		handle, err := box.Exec(ctx, cellruntime.ExecRequest{Argv: []string{"/bin/sleep", "30"}, TimeoutMS: 30000})
		if err != nil {
			t.Fatal(err)
		}
		if err := box.Kill(ctx); err != nil {
			t.Fatalf("Kill mid-exec: %v", err)
		}
		for range handle.Output() {
		}
		_, _ = handle.Wait(ctx)
		if err := box.Destroy(ctx); err != nil {
			t.Fatalf("Destroy after Kill: %v", err)
		}
		assertNoSubdirs(t, sessionsCgroupDir)
		assertEmptyDir(t, sessionsDataDir)
	})

	t.Run("startup_reconcile_cleans_unreleased_native_session", func(t *testing.T) {
		orphanID := fmt.Sprintf("ses_chaos_orphan_%d", time.Now().UnixNano())
		_, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          orphanID,
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 32},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		// Simulate process restart with a fresh Runtime instance that has no in-memory handle.
		restarted, err := nativeruntime.NewRuntime(dataDir, cgroups)
		if err != nil {
			t.Fatal(err)
		}
		if err := reconcile.Startup(ctx, restarted, nil); err != nil {
			t.Fatalf("reconcile.Startup: %v", err)
		}
		assertNoSubdirs(t, sessionsCgroupDir)
		assertEmptyDir(t, sessionsDataDir)
	})

	t.Run("repeated_cycles_leak_no_fds_cgroups_or_dirs", func(t *testing.T) {
		// Warm up one cycle so runtime/net caches stabilize before counting FDs.
		warm, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          fmt.Sprintf("ses_leak_warm_%d", time.Now().UnixNano()),
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 32},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = warm.Destroy(ctx)

		beforeFDs := countOpenFDs(t)
		for i := 0; i < 5; i++ {
			box, err := backend.Create(ctx, cellruntime.SandboxSpec{
				ID:          fmt.Sprintf("ses_leak_%d_%d", i, time.Now().UnixNano()),
				Image:       image,
				Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 32},
				NetworkMode: "none",
			})
			if err != nil {
				t.Fatalf("cycle %d Create: %v", i, err)
			}
			res, _ := execInBox(t, ctx, box, cellruntime.ExecRequest{Argv: []string{"/bin/echo", "ok"}})
			if res.ExitCode != 0 {
				t.Fatalf("cycle %d Exec exit=%d", i, res.ExitCode)
			}
			if err := box.Destroy(ctx); err != nil {
				t.Fatalf("cycle %d Destroy: %v", i, err)
			}
		}
		afterFDs := countOpenFDs(t)
		if afterFDs > beforeFDs+2 {
			t.Fatalf("file descriptor leak across 5 cycles: before=%d after=%d", beforeFDs, afterFDs)
		}
		assertNoSubdirs(t, sessionsCgroupDir)
		assertEmptyDir(t, sessionsDataDir)
	})
}

func TestNativeTerminalReconnectLive(t *testing.T) {
	backend, _ := newDelegatedNativeRuntime(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "native-terminal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	key, err := auth.Generate("key_native_term", "native-term-admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateKey(context.Background(), sqlite.KeyRecord{
		ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Backend = "native"
	srv := httptest.NewServer(api.NewHandlerWithConfig(db, backend, "native", cfg))
	defer srv.Close()

	client := srv.Client()
	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", strings.NewReader(`{"image":"alpine:latest"}`))
	createReq.Header.Set("Authorization", "Bearer "+key.Secret)
	createReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	defer func() {
		delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/sessions/"+created.ID, nil)
		delReq.Header.Set("Authorization", "Bearer "+key.Secret)
		if delResp, err := client.Do(delReq); err == nil {
			_ = delResp.Body.Close()
		}
	}()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/sessions/" + created.ID + "/terminal"
	headers := http.Header{"Authorization": {"Bearer " + key.Secret}}

	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("dial first terminal ws: %v", err)
	}
	if err := conn1.WriteMessage(websocket.BinaryMessage, []byte("printf 'native-before-%s\\n' disconnect\r")); err != nil {
		t.Fatal(err)
	}
	readUntilContains(t, conn1, "native-before-disconnect")
	_ = conn1.Close()

	// Wait briefly for hub.detach to complete, then reconnect within the 30s grace window.
	var conn2 *websocket.Conn
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn2, _, err = websocket.DefaultDialer.Dial(wsURL, headers)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect terminal ws failed: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer conn2.Close()

	readUntilContains(t, conn2, "native-before-disconnect")
	if err := conn2.WriteMessage(websocket.BinaryMessage, []byte("printf 'native-after-%s\\n' reconnect\r")); err != nil {
		t.Fatal(err)
	}
	readUntilContains(t, conn2, "native-after-reconnect")
}

func readUntilContains(t *testing.T, conn *websocket.Conn, substr string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	var collected strings.Builder
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read ws waiting for %q (got %q): %v", substr, collected.String(), err)
		}
		collected.Write(payload)
		if strings.Contains(collected.String(), substr) {
			return
		}
	}
}

func assertNoSubdirs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e.Name())
		}
	}
	if len(subdirs) != 0 {
		t.Fatalf("expected no child cgroups in %s, found %v", dir, subdirs)
	}
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected %s to be empty, found %v", dir, names)
	}
}
