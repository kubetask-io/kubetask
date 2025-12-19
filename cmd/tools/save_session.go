// Copyright Contributors to the KubeTask project

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// Environment variable names for save-session
const (
	envTaskName      = "TASK_NAME"
	envTaskNamespace = "TASK_NAMESPACE"
	envWorkspaceDir  = "WORKSPACE_DIR"
	envPVCMountPath  = "PVC_MOUNT_PATH"
	envSignalFile    = "SIGNAL_FILE"
)

// Default values for save-session
const (
	defaultPVCMountPath = "/pvc"
	defaultSignalFile   = "/signal/.agent-done"
	defaultWorkspace    = "/workspace"
)

func init() {
	rootCmd.AddCommand(saveSessionCmd)
}

var saveSessionCmd = &cobra.Command{
	Use:   "save-session",
	Short: "Save workspace to PVC for session persistence",
	Long: `save-session waits for the agent container to complete, then copies
the workspace directory to a PVC for later session resume.

It works by:
  1. Waiting for a signal file indicating agent completion
  2. Copying the workspace directory to the PVC
  3. Exiting after the copy is complete

Environment variables:
  TASK_NAME       Name of the Task (required)
  TASK_NAMESPACE  Namespace of the Task (required)
  WORKSPACE_DIR   Workspace directory to save, default: /workspace
  PVC_MOUNT_PATH  PVC mount path, default: /pvc
  SIGNAL_FILE     Signal file from agent wrapper, default: /signal/.agent-done`,
	RunE: runSaveSession,
}

func runSaveSession(cmd *cobra.Command, args []string) error {
	// Get required environment variables
	taskName := os.Getenv(envTaskName)
	if taskName == "" {
		return fmt.Errorf("%s environment variable is required", envTaskName)
	}

	taskNamespace := os.Getenv(envTaskNamespace)
	if taskNamespace == "" {
		return fmt.Errorf("%s environment variable is required", envTaskNamespace)
	}

	// Get optional environment variables with defaults
	workspaceDir := getEnvOrDefault(envWorkspaceDir, defaultWorkspace)
	pvcMountPath := getEnvOrDefault(envPVCMountPath, defaultPVCMountPath)
	signalFile := getEnvOrDefault(envSignalFile, defaultSignalFile)

	// Destination directory on PVC
	destDir := filepath.Join(pvcMountPath, taskNamespace, taskName)

	fmt.Println("save-session: Waiting for agent container to complete...")
	fmt.Printf("  Task: %s/%s\n", taskNamespace, taskName)
	fmt.Printf("  Workspace: %s\n", workspaceDir)
	fmt.Printf("  Destination: %s\n", destDir)
	fmt.Printf("  Signal file: %s\n", signalFile)

	// Wait for signal file from agent wrapper
	if err := waitForSignal(signalFile); err != nil {
		return fmt.Errorf("failed waiting for agent completion: %w", err)
	}

	fmt.Println("save-session: Agent completed, saving workspace to PVC...")

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Copy workspace to PVC
	// Using cp -r to preserve directory structure
	// The trailing /. ensures we copy contents, not the directory itself
	srcPath := workspaceDir + "/."
	copyCmd := exec.Command("cp", "-r", srcPath, destDir)
	copyCmd.Stdout = os.Stdout
	copyCmd.Stderr = os.Stderr

	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy workspace: %w", err)
	}

	fmt.Printf("save-session: Workspace saved to %s\n", destDir)
	fmt.Println("save-session: Session persistence complete")

	return nil
}

func waitForSignal(signalFile string) error {
	// Poll for signal file with timeout
	// The agent wrapper creates this file when it completes
	maxWait := 24 * time.Hour // Maximum wait time (1 day)
	pollInterval := 2 * time.Second
	startTime := time.Now()

	for {
		if _, err := os.Stat(signalFile); err == nil {
			// Signal file exists, agent has completed
			return nil
		}

		if time.Since(startTime) > maxWait {
			return fmt.Errorf("timeout waiting for signal file after %v", maxWait)
		}

		time.Sleep(pollInterval)
	}
}
