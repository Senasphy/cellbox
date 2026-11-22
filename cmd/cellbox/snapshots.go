package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/senasphy/cellbox/internal/api"
	"github.com/spf13/cobra"
)

type snapshotCLIResponse struct {
	ID               string            `json:"id"`
	SourceSessionID  string            `json:"source_session_id"`
	ParentSnapshotID string            `json:"parent_snapshot_id,omitempty"`
	Size             int64             `json:"size_bytes"`
	Flattened        bool              `json:"flattened,omitempty"`
	CreatedAt        string            `json:"created_at"`
	Labels           map[string]string `json:"labels"`
}

func newSnapshotCommand() *cobra.Command {
	var endpoint string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "snapshot SESSION_ID",
		Short: "Create a filesystem snapshot of a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, base, key, err := imageClient(endpoint)
			if err != nil {
				return err
			}
			path := "/v1/sessions/" + url.PathEscape(args[0]) + "/snapshots"
			var result snapshotCLIResponse
			if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodPost, path, nil, &result); err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created snapshot %s from %s (%d bytes)\n", result.ID, result.SourceSessionID, result.Size)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON")
	return cmd
}

func newForkCommand() *cobra.Command {
	var endpoint, snapshotID string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "fork SESSION_ID",
		Short: "Create a session from a snapshot or snapshot the source and fork it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, base, key, err := imageClient(endpoint)
			if err != nil {
				return err
			}
			var body any = map[string]string{}
			if snapshotID != "" {
				body = map[string]string{"snapshot_id": snapshotID}
			}
			path := "/v1/sessions/" + url.PathEscape(args[0]) + "/fork"
			var result api.SessionResponse
			if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodPost, path, body, &result); err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created fork %s from %s (snapshot %s)\n", result.ID, args[0], result.ParentSnapshotID)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "fork from this existing snapshot instead of snapshotting the source")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON")
	return cmd
}
