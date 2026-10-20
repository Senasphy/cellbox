package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/senasphy/cellbox/internal/api"
	"github.com/spf13/cobra"
)

type sessionListResponse struct {
	Items      []api.SessionResponse `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func newSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "List and destroy sandbox sessions"}
	cmd.AddCommand(newSessionListCommand(), newSessionRemoveCommand())
	return cmd
}

func newSessionListCommand() *cobra.Command {
	var endpoint, cursor string
	var limit int
	var jsonOutput bool
	cmd := &cobra.Command{Use: "ls", Short: "List sessions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		query := url.Values{}
		if limit > 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		path := "/v1/sessions"
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		var result sessionListResponse
		if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodGet, path, nil, &result); err != nil {
			return err
		}
		if jsonOutput {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		if len(result.Items) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No sessions.")
			return nil
		}
		for _, item := range result.Items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ID, item.State, item.Image, item.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
		}
		if result.NextCursor != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Next cursor: %s\n", result.NextCursor)
		}
		return nil
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum sessions to return (1-200)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON")
	return cmd
}

func newSessionRemoveCommand() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{Use: "rm SESSION_ID", Short: "Destroy a session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		if strings.Contains(args[0], "/") {
			return fmt.Errorf("invalid session id")
		}
		if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodDelete, "/v1/sessions/"+url.PathEscape(args[0]), nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Destroyed %s\n", args[0])
		return nil
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	return cmd
}
