package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/api"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/dashboard"
	"github.com/senasphy/cellbox/internal/doctor"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/logging"
	warmruntime "github.com/senasphy/cellbox/internal/pool"
	"github.com/senasphy/cellbox/internal/reconcile"
	"github.com/senasphy/cellbox/internal/runtime"
	dockerruntime "github.com/senasphy/cellbox/internal/runtime/docker"
	nativeruntime "github.com/senasphy/cellbox/internal/runtime/native"
	sqlitestore "github.com/senasphy/cellbox/internal/store/sqlite"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRoot().Execute(); err != nil {
		var processExit *commandExitError
		if errors.As(err, &processExit) {
			fmt.Fprintln(os.Stderr, processExit.Error())
			os.Exit(processExit.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type commandExitError struct {
	code int
}

func (e *commandExitError) Error() string {
	return fmt.Sprintf("command exited with status %d", e.code)
}
func newRoot() *cobra.Command {
	root := &cobra.Command{Use: "cellbox", Short: "Self-hostable disposable computers for AI agents", SilenceUsage: true}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print version", Run: func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version) }})
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Check host prerequisites", RunE: func(cmd *cobra.Command, _ []string) error {
		lines := doctor.Check()
		for _, line := range lines {
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return nil
	}})
	root.AddCommand(newKeysCommand())
	root.AddCommand(newImagesCommand())
	root.AddCommand(newSessionsCommand())
	root.AddCommand(newSnapshotCommand())
	root.AddCommand(newEgressForwarderCommand())
	root.AddCommand(newForkCommand())
	root.AddCommand(newShellCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newMCPCommand())
	var cfgPath string
	var listen, dataDir, backend, logLevel, logFormat string
	var allowRemote bool
	serve := &cobra.Command{Use: "serve", Short: "Start the cellbox service", RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("listen") {
			cfg.Listen = listen
		}
		if cmd.Flags().Changed("data-dir") {
			cfg.DataDir = dataDir
		}
		if cmd.Flags().Changed("backend") {
			cfg.Backend = backend
		}
		if cmd.Flags().Changed("log-level") {
			cfg.Log.Level = logLevel
		}
		if cmd.Flags().Changed("log-format") {
			cfg.Log.Format = logFormat
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		logger := logging.New(cfg.Log.Level, cfg.Log.Format)
		if err = os.MkdirAll(cfg.DataDir, 0o700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		db, err := sqlitestore.Open(filepath.Join(cfg.DataDir, "cellbox.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		if err = db.Ping(ctx); err != nil {
			return err
		}
		keys, err := db.ListKeys(cmd.Context())
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			created, err := newAPIKey("Initial administrator", "admin")
			if err != nil {
				return err
			}
			if err = db.CreateKey(cmd.Context(), created.record); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "admin key (shown once):", created.Secret)
		}
		logger.Info("foundations ready", "listen", cfg.Listen, "data_dir", cfg.DataDir, "backend", cfg.Backend)
		if !loopbackAddress(cfg.Listen) && !allowRemote {
			return fmt.Errorf("refusing non-loopback listen address %q without --allow-remote", cfg.Listen)
		}
		if !loopbackAddress(cfg.Listen) && cfg.TLS.Cert == "" {
			logger.Warn("service is bound to a non-loopback address without TLS")
		}
		var runtimeBackend runtime.Runtime
		switch cfg.Backend {
		case "docker":
			runtimeBackend = dockerruntime.New(cfg.DockerSocket)
		case "native":
			cgroupPath, cgroupErr := nativeruntime.CurrentCgroupPath()
			if cgroupErr != nil {
				return fmt.Errorf("locate delegated cgroup: %w", cgroupErr)
			}
			cgroups, cgroupErr := nativeruntime.InitializeCgroupRoot(cgroupPath, os.Getpid())
			if cgroupErr != nil {
				return fmt.Errorf("initialize native cgroups: %w", cgroupErr)
			}
			runtimeBackend, cgroupErr = nativeruntime.NewRuntime(cfg.DataDir, cgroups)
			if cgroupErr != nil {
				return fmt.Errorf("initialize native runtime: %w", cgroupErr)
			}
		}
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err = reconcile.Startup(reconcileCtx, runtimeBackend, db); err != nil {
			reconcileCancel()
			return fmt.Errorf("startup reconciliation failed: %w", err)
		}
		reconcileCancel()
		var warmPool *warmruntime.Manager
		if cfg.Pool.Enabled {
			warmPool = warmruntime.New(runtimeBackend, cfg.Pool)
			warmPool.Prime(cfg.Pool.Images)
			defer func() {
				poolCtx, poolCancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer poolCancel()
				if closeErr := warmPool.Close(poolCtx); closeErr != nil {
					logger.Error("warm pool shutdown failed", "error", closeErr)
				}
			}()
		}
		var apiHandler http.Handler
		if warmPool != nil {
			apiHandler = api.NewHandlerWithPool(db, runtimeBackend, cfg.Backend, cfg, warmPool)
		} else {
			apiHandler = api.NewHandlerWithConfig(db, runtimeBackend, cfg.Backend, cfg)
		}
		handler := dashboard.Wrap(apiHandler)
		srv := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
		serveCtx, stop := signalContext(cmd.Context())
		defer stop()
		if reaperBackend, ok := runtimeBackend.(interface{ SandboxByID(string) runtime.Sandbox }); ok {
			go reconcile.Run(serveCtx, reaperBackend, db, 5*time.Second, func(reaperErr error) {
				logger.Error("session TTL sweep failed", "error", reaperErr)
			})
		}
		go reconcile.RunRetention(serveCtx, db, cfg.Retention.OutputDays, cfg.Retention.AuditDays, func(retentionErr error) {
			logger.Error("retention sweep failed", "error", retentionErr)
		})
		result := make(chan error, 1)
		go func() {
			var e error
			if cfg.TLS.Cert != "" {
				e = srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
			} else {
				e = srv.ListenAndServe()
			}
			if e == http.ErrServerClosed {
				e = nil
			}
			result <- e
		}()
		logger.Info("listening", "address", cfg.Listen, "dashboard", "/")
		select {
		case err = <-result:
			if err != nil {
				return fmt.Errorf("serve HTTP: %w", err)
			}
			return nil
		case <-serveCtx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err = srv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shut down HTTP server: %w", err)
			}
			return nil
		}
	}}
	serve.Flags().StringVar(&cfgPath, "config", "", "configuration file")
	serve.Flags().StringVar(&listen, "listen", "", "HTTP listen address")
	serve.Flags().StringVar(&dataDir, "data-dir", "", "service data directory")
	serve.Flags().StringVar(&backend, "backend", "", "runtime backend (native or docker)")
	serve.Flags().StringVar(&logLevel, "log-level", "", "log level")
	serve.Flags().StringVar(&logFormat, "log-format", "", "log format (json or text)")
	serve.Flags().BoolVar(&allowRemote, "allow-remote", false, "allow a non-loopback listen address")
	root.AddCommand(serve)
	return root
}

func newRunCommand() *cobra.Command {
	var image string
	var endpoint string
	cmd := &cobra.Command{
		Use:     "run --image IMAGE -- COMMAND [ARG...]",
		Short:   "Create a session, stream a command, and destroy the session",
		Args:    cobra.MinimumNArgs(1),
		Example: "cellbox run --image python:3.12 -- python -c 'print(\"hello\")'",
		RunE: func(cmd *cobra.Command, args []string) error {
			key := os.Getenv("CELLBOX_KEY")
			if key == "" {
				return fmt.Errorf("CELLBOX_KEY is required")
			}
			if endpoint == "" {
				endpoint = "http://127.0.0.1:8080"
			}
			if image == "" {
				return fmt.Errorf("--image is required")
			}
			client := &http.Client{Timeout: 0}
			base := strings.TrimRight(endpoint, "/")
			var session struct {
				ID string `json:"id"`
			}
			if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodPost, "/v1/sessions", map[string]any{"image": image, "network_policy": map[string]string{"mode": "none"}}, &session); err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			if session.ID == "" {
				return fmt.Errorf("create session returned no id")
			}
			defer func() {
				_ = runJSONRequest(context.Background(), client, base, key, http.MethodDelete, "/v1/sessions/"+session.ID, nil, nil)
			}()
			var execution struct {
				ID        string `json:"id"`
				StreamURL string `json:"stream_url"`
			}
			path := "/v1/sessions/" + session.ID + "/execs"
			if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodPost, path, map[string]any{"argv": args}, &execution); err != nil {
				return fmt.Errorf("start command: %w", err)
			}
			if execution.ID == "" {
				return fmt.Errorf("exec response returned no id")
			}
			streamPath := execution.StreamURL
			if streamPath == "" {
				streamPath = path + "/" + execution.ID + "/stream"
			}
			return streamRunOutput(cmd.Context(), client, base, key, streamPath, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "OCI image to run")
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	return cmd
}

func runJSONRequest(ctx context.Context, client *http.Client, base, key, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	if output != nil {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	return nil
}

func streamRunOutput(ctx context.Context, client *http.Client, base, key, target string, stdout, stderr io.Writer) error {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		// Stream URLs are normally relative; only accept absolute URLs on the configured origin.
		if !strings.HasPrefix(target, base+"/") {
			return fmt.Errorf("stream URL is outside configured service")
		}
		target = strings.TrimPrefix(target, base)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("stream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	event := ""
	var data strings.Builder
	var exitCode int
	gotExit := false
	commandFailed := false
	flushEvent := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		payload := strings.TrimSpace(data.String())
		switch event {
		case "output":
			var item struct {
				Stream string `json:"stream"`
				Data   string `json:"data"`
			}
			if err := json.Unmarshal([]byte(payload), &item); err != nil {
				return err
			}
			decoded, err := base64.StdEncoding.DecodeString(item.Data)
			if err != nil {
				return err
			}
			writer := stdout
			if item.Stream == "stderr" {
				writer = stderr
			}
			if _, err := writer.Write(decoded); err != nil {
				return err
			}
		case "exit", "error":
			var result struct {
				ExitCode *int   `json:"exit_code"`
				Signal   *int   `json:"signal"`
				State    string `json:"state"`
			}
			if err := json.Unmarshal([]byte(payload), &result); err != nil {
				return err
			}
			if result.ExitCode != nil {
				exitCode = *result.ExitCode
			} else if result.Signal != nil && *result.Signal > 0 {
				exitCode = 128 + *result.Signal
			} else if event == "error" || result.State == "failed" {
				exitCode = 1
			}
			commandFailed, gotExit = event == "error" || result.State == "failed", true
		}
		event, data = "", strings.Builder{}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flushEvent(); err != nil {
		return err
	}
	if !gotExit {
		return fmt.Errorf("stream ended before command exit event")
	}
	if exitCode != 0 {
		return &commandExitError{code: exitCode}
	}
	if commandFailed {
		return &commandExitError{code: 1}
	}
	return nil
}

func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

type generatedKey struct {
	Secret string
	record sqlitestore.KeyRecord
}

func newAPIKey(name, scope string) (generatedKey, error) {
	keyID, err := id.New("key")
	if err != nil {
		return generatedKey{}, err
	}
	key, err := auth.Generate(keyID, name, scope)
	if err != nil {
		return generatedKey{}, err
	}
	return generatedKey{Secret: key.Secret, record: sqlitestore.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}}, nil
}

func newKeysCommand() *cobra.Command {
	keys := &cobra.Command{Use: "keys", Short: "Manage local API keys"}
	var dataDir string
	addDataDir := func(c *cobra.Command) {
		c.Flags().StringVar(&dataDir, "data-dir", "", "data directory (defaults to configured data_dir)")
	}
	create := &cobra.Command{Use: "create", Short: "Create an API key", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("expected at most one key name")
		}
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("data-dir") {
			cfg.DataDir = dataDir
		}
		if err = os.MkdirAll(cfg.DataDir, 0o700); err != nil {
			return err
		}
		db, err := sqlitestore.Open(filepath.Join(cfg.DataDir, "cellbox.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		name := "API key"
		if len(args) == 1 {
			name = args[0]
		}
		key, err := newAPIKey(name, "admin")
		if err != nil {
			return err
		}
		if err = db.CreateKey(cmd.Context(), key.record); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), key.Secret)
		return nil
	}}
	list := &cobra.Command{Use: "ls", Aliases: []string{"list"}, Short: "List API keys", RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("data-dir") {
			cfg.DataDir = dataDir
		}
		db, err := sqlitestore.Open(filepath.Join(cfg.DataDir, "cellbox.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		items, err := db.ListKeys(cmd.Context())
		if err != nil {
			return err
		}
		for _, k := range items {
			state := "active"
			if k.RevokedAt != nil {
				state = "revoked"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", k.ID, k.Name, k.Prefix, k.Scope, state)
		}
		return nil
	}}
	revoke := &cobra.Command{Use: "revoke <key-id>", Args: cobra.ExactArgs(1), Short: "Revoke an API key", RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("data-dir") {
			cfg.DataDir = dataDir
		}
		db, err := sqlitestore.Open(filepath.Join(cfg.DataDir, "cellbox.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.RevokeKey(cmd.Context(), args[0])
	}}
	for _, c := range []*cobra.Command{create, list, revoke} {
		addDataDir(c)
	}
	keys.AddCommand(create, list, revoke)
	return keys
}
