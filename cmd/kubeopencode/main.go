// Copyright Contributors to the KubeTask project

// kubetask is the unified binary for KubeTask, providing both controller
// and infrastructure tool functionality in a single image.
//
// Available commands:
//   - controller:    Start the Kubernetes controller
//   - git-init:      Clone Git repositories for Git Context
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kubetask",
	Short: "KubeTask - Kubernetes-native AI task execution",
	Long: `KubeTask is a Kubernetes-native system for executing AI-powered tasks.

This unified binary provides:
  controller      Start the Kubernetes controller
  git-init        Clone Git repositories for Git Context

Examples:
  # Start the controller
  kubetask controller --metrics-bind-address=:8080

  # Clone a Git repository (used in init containers)
  kubetask git-init`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
