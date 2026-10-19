package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/senasphy/cellbox/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	var transport, listen, image string
	var memory int
	cmd := &cobra.Command{Use: "mcp", Short: "Run the cellbox Model Context Protocol server", RunE: func(cmd *cobra.Command, _ []string) error {
		key := os.Getenv("CELLBOX_KEY")
		if key == "" {
			return fmt.Errorf("CELLBOX_KEY is required")
		}
		base := os.Getenv("CELLBOX_URL")
		if base == "" {
			base = "http://127.0.0.1:8080"
		}
		if transport == "" {
			transport = "stdio"
		}
		cfg := mcpserver.Config{URL: base, Key: key, Image: image, MemoryMB: memory}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if transport == "stdio" {
			server, cleanup := mcpserver.NewWithCleanup(cfg)
			defer func() {
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = cleanup(c)
			}()
			return server.Run(ctx, &mcp.StdioTransport{})
		}
		if transport != "http" {
			return fmt.Errorf("transport must be stdio or http")
		}
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			server, cleanup := mcpserver.NewWithCleanup(cfg)
			go func() {
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					for session := range server.Sessions() {
						_ = session.Wait()
						c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						_ = cleanup(c)
						return
					}
					<-ticker.C
				}
			}()
			return server
		}, &mcp.StreamableHTTPOptions{SessionTimeout: time.Hour})
		httpServer := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go func() { <-ctx.Done(); _ = httpServer.Shutdown(context.Background()) }()
		return httpServer.ListenAndServe()
	}}
	cmd.Flags().StringVar(&transport, "transport", "stdio", "MCP transport: stdio or http")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8081", "streamable HTTP listen address")
	cmd.Flags().StringVar(&image, "image", "alpine:latest", "sandbox image")
	cmd.Flags().IntVar(&memory, "memory-mb", 512, "sandbox memory limit")
	return cmd
}
