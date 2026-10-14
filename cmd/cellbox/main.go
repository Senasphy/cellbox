package main

import (
	"context"
	"fmt"
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
	"github.com/senasphy/cellbox/internal/doctor"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/logging"
	dockerruntime "github.com/senasphy/cellbox/internal/runtime/docker"
	sqlitestore "github.com/senasphy/cellbox/internal/store/sqlite"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
		if err = os.MkdirAll(cfg.DataDir, 0700); err != nil {
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
		var handler http.Handler
		if cfg.Backend == "docker" {
			handler = api.NewHandlerWithConfig(db, dockerruntime.New(cfg.DockerSocket), cfg.Backend, cfg)
		} else {
			handler = api.NewHandlerWithConfig(db, nil, cfg.Backend, cfg)
		}
		srv := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
		serveCtx, stop := signalContext(cmd.Context())
		defer stop()
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
		if err = os.MkdirAll(cfg.DataDir, 0700); err != nil {
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
