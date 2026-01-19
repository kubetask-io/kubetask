// Copyright Contributors to the KubeOpenCode project

// kubeopencode is the unified binary for KubeOpenCode, providing both controller
// and infrastructure tool functionality in a single image.
//
// Available commands:
//   - controller:    Start the Kubernetes controller
//   - git-init:      Clone Git repositories for Git Context
//   - context-init:  Copy ConfigMap content to workspace
//   - url-fetch:     Fetch content from remote URLs for URL Context
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kubeopencode",
	Short: "KubeOpenCode - Kubernetes-native AI task execution",
	Long: `KubeOpenCode is a Kubernetes-native system for executing AI-powered tasks.

This unified binary provides:
  controller     Start the Kubernetes controller
  git-init       Clone Git repositories for Git Context
  context-init   Copy ConfigMap content to workspace
  url-fetch      Fetch content from remote URLs for URL Context

Examples:
  # Start the controller
  kubeopencode controller --metrics-bind-address=:8080

  # Clone a Git repository (used in init containers)
  kubeopencode git-init

  # Fetch URL content (used in init containers)
  kubeopencode url-fetch`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
