// Copyright Contributors to the KubeTask project

// kubetask-tools provides utility commands for KubeTask infrastructure.
// It combines multiple tools into a single binary with subcommands:
//   - git-init: Clone Git repositories for Git Context
//   - save-session: Save workspace to PVC for session persistence
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kubetask-tools",
	Short: "KubeTask infrastructure tools",
	Long: `kubetask-tools provides utility commands for KubeTask infrastructure.

Available commands:
  git-init      Clone Git repositories for Git Context
  save-session  Save workspace to PVC for session persistence`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
