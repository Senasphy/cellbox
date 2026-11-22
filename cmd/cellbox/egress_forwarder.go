package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/senasphy/cellbox/internal/egress"
	"github.com/spf13/cobra"
)

func newEgressForwarderCommand() *cobra.Command {
	var listenAddress, socketPath string
	command := &cobra.Command{
		Use:    "_egress-forwarder",
		Short:  "Bridge sandbox loopback proxy traffic to a host Unix socket",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			forwarder, err := egress.NewForwarder(listenAddress, socketPath)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return forwarder.Serve(ctx)
		},
	}
	command.Flags().StringVar(&listenAddress, "listen", "127.0.0.1:3128", "sandbox loopback address")
	command.Flags().StringVar(&socketPath, "socket", "/run/cellbox/egress.sock", "mounted host proxy Unix socket")
	return command
}
