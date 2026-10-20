package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/spf13/cobra"
)

type imageListResponse struct {
	Images []runtime.ImageInfo `json:"images"`
}

func newImagesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "images", Short: "Manage cached container images"}
	cmd.AddCommand(newImageListCommand())
	cmd.AddCommand(newImagePullCommand())
	cmd.AddCommand(newImageRemoveCommand())
	return cmd
}

func imageClient(endpoint string) (*http.Client, string, string, error) {
	key := os.Getenv("CELLBOX_KEY")
	if key == "" {
		return nil, "", "", fmt.Errorf("CELLBOX_KEY is required")
	}
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080"
	}
	return &http.Client{}, strings.TrimRight(endpoint, "/"), key, nil
}

func newImageListCommand() *cobra.Command {
	var endpoint string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "ls", Short: "List cached container images", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		var result imageListResponse
		if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodGet, "/v1/images", nil, &result); err != nil {
			return err
		}
		if jsonOutput {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		if len(result.Images) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No cached images.")
			return nil
		}
		for _, image := range result.Images {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", image.Reference, image.Digest, image.Size)
		}
		return nil
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON")
	return cmd
}

func newImagePullCommand() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{Use: "pull IMAGE", Short: "Pull an image into the runtime cache", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		var image runtime.ImageInfo
		if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodPost, "/v1/images", map[string]string{"reference": args[0]}, &image); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", image.Reference, image.Digest, image.Size)
		return err
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	return cmd
}

func newImageRemoveCommand() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{Use: "rm IMAGE", Short: "Remove an image from the runtime cache", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, key, err := imageClient(endpoint)
		if err != nil {
			return err
		}
		path := "/v1/images?" + url.Values{"reference": []string{args[0]}}.Encode()
		if err := runJSONRequest(cmd.Context(), client, base, key, http.MethodDelete, path, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", args[0])
		return nil
	}}
	cmd.Flags().StringVar(&endpoint, "url", os.Getenv("CELLBOX_URL"), "cellbox service URL (defaults to CELLBOX_URL or loopback)")
	return cmd
}
