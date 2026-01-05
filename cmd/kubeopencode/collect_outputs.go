// Copyright Contributors to the KubeOpenCode project

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Environment variable names for collect-outputs
const (
	envOutputSpec = "OUTPUT_SPEC"
)

// OutputParameterSpec mirrors the API type for parsing OUTPUT_SPEC JSON
type OutputParameterSpec struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Default *string `json:"default,omitempty"`
}

// OutputSpec mirrors the API type for parsing OUTPUT_SPEC JSON
type OutputSpec struct {
	Parameters []OutputParameterSpec `json:"parameters,omitempty"`
}

// terminationOutput is the JSON structure written to /dev/termination-log
type terminationOutput struct {
	Parameters map[string]string `json:"parameters,omitempty"`
}

func init() {
	rootCmd.AddCommand(collectOutputsCmd)
}

var collectOutputsCmd = &cobra.Command{
	Use:   "collect-outputs",
	Short: "Collect output parameters from files and write to termination log",
	Long: `collect-outputs is a sidecar command that waits for the main container to exit,
then reads output parameter values from files and writes them to the termination log.

This command runs as a sidecar container in the same Pod as the agent container.
It uses shared PID namespace to detect when the main container (agent) exits.

Environment variables:
  WORKSPACE_DIR   Base directory for resolving relative paths (default: /workspace)
  OUTPUT_SPEC     JSON specification of output parameters to collect

Example OUTPUT_SPEC:
  {"parameters": [
    {"name": "pr-url", "path": ".outputs/pr-url"},
    {"name": "summary", "path": ".outputs/summary", "default": "No summary"}
  ]}

The collected parameters are written to /dev/termination-log as JSON:
  {"parameters": {"pr-url": "https://...", "summary": "..."}}`,
	RunE: runCollectOutputs,
}

func runCollectOutputs(cmd *cobra.Command, args []string) error {
	// Get configuration from environment variables
	workspaceDir := getEnvOrDefault(envWorkspaceDir, defaultWorkspaceDir)
	outputSpecJSON := os.Getenv(envOutputSpec)

	fmt.Println("collect-outputs: Starting output collector sidecar...")
	fmt.Printf("  Workspace: %s\n", workspaceDir)

	// Parse output specification
	if outputSpecJSON == "" {
		fmt.Println("collect-outputs: No OUTPUT_SPEC provided, nothing to collect")
		return nil
	}

	var spec OutputSpec
	if err := json.Unmarshal([]byte(outputSpecJSON), &spec); err != nil {
		return fmt.Errorf("failed to parse OUTPUT_SPEC: %w", err)
	}

	if len(spec.Parameters) == 0 {
		fmt.Println("collect-outputs: No output parameters defined, nothing to collect")
		return nil
	}

	fmt.Printf("  Output parameters: %d\n", len(spec.Parameters))
	for _, p := range spec.Parameters {
		fmt.Printf("    - %s: %s\n", p.Name, p.Path)
	}

	// Wait for main container (agent) to exit
	fmt.Println("collect-outputs: Waiting for agent container to exit...")
	if err := waitForAgentExit(); err != nil {
		return fmt.Errorf("error waiting for agent exit: %w", err)
	}
	fmt.Println("collect-outputs: Agent container exited, collecting outputs...")

	// Collect parameters from files
	params := make(map[string]string)
	for _, p := range spec.Parameters {
		value, found := readOutputParameter(workspaceDir, p)
		if found {
			params[p.Name] = value
			fmt.Printf("collect-outputs: Collected %s = %q\n", p.Name, truncateForLog(value, 50))
		} else if p.Default != nil {
			params[p.Name] = *p.Default
			fmt.Printf("collect-outputs: Using default for %s = %q\n", p.Name, *p.Default)
		} else {
			fmt.Printf("collect-outputs: Skipping %s (file not found, no default)\n", p.Name)
		}
	}

	// Write to termination log
	if len(params) > 0 {
		output := terminationOutput{Parameters: params}
		if err := writeTerminationLog(output); err != nil {
			return fmt.Errorf("failed to write termination log: %w", err)
		}
		fmt.Printf("collect-outputs: Wrote %d parameters to termination log\n", len(params))
	} else {
		fmt.Println("collect-outputs: No parameters collected, nothing to write")
	}

	fmt.Println("collect-outputs: Done!")
	return nil
}

// waitForAgentExit waits for the agent container to exit by monitoring the /proc filesystem.
// In a shared PID namespace, we can see all processes. We look for the agent process
// by checking for PID 1's children (the agent is typically the first user process).
func waitForAgentExit() error {
	// In shared PID namespace, the pause container or init process is PID 1.
	// The agent container runs as a child process. We poll to detect when
	// the agent process terminates.

	// Strategy: Wait until we're the only non-init process remaining.
	// This is a simple heuristic that works for most cases.

	pollInterval := 1 * time.Second
	maxWait := 24 * time.Hour // Reasonable maximum for long-running tasks

	startTime := time.Now()

	for {
		if time.Since(startTime) > maxWait {
			return fmt.Errorf("timeout waiting for agent to exit after %v", maxWait)
		}

		// Check if there are any processes other than ourselves and init
		// In shared PID namespace, we can read /proc to see all processes
		agentRunning, err := isAgentContainerRunning()
		if err != nil {
			// Log error but continue polling
			fmt.Printf("collect-outputs: Warning: error checking process status: %v\n", err)
		}

		if !agentRunning {
			return nil
		}

		time.Sleep(pollInterval)
	}
}

// isAgentContainerRunning checks if the agent container is still running.
// It looks for the "agent" container process in /proc by checking cmdline.
func isAgentContainerRunning() (bool, error) {
	// Read all process directories in /proc
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("failed to read /proc: %w", err)
	}

	// Our own PID
	myPid := os.Getpid()

	// Count processes that are not init (1) or ourselves
	for _, entry := range entries {
		// Skip non-numeric entries
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) == 0 || name[0] < '0' || name[0] > '9' {
			continue
		}

		// Parse PID
		var pid int
		if _, err := fmt.Sscanf(name, "%d", &pid); err != nil {
			continue
		}

		// Skip PID 1 (init/pause) and ourselves
		if pid == 1 || pid == myPid {
			continue
		}

		// Check if this is a main process (not a thread) by checking if /proc/[pid]/cmdline exists
		cmdlinePath := filepath.Join("/proc", name, "cmdline")
		cmdline, err := os.ReadFile(cmdlinePath)
		if err != nil {
			// Process may have exited, continue
			continue
		}

		// Skip empty cmdlines (kernel threads)
		if len(cmdline) == 0 {
			continue
		}

		// Check if this looks like the agent container's command
		// The agent container typically runs with a command that includes "sh", "bash", or the actual agent binary
		cmdStr := string(cmdline)

		// If we find any user process that's not ourselves, assume agent is still running
		// Skip our own kubeopencode collect-outputs process
		if !strings.Contains(cmdStr, "collect-outputs") {
			return true, nil
		}
	}

	return false, nil
}

// readOutputParameter reads a parameter value from a file.
// Returns the trimmed content and whether the file was found.
func readOutputParameter(workspaceDir string, param OutputParameterSpec) (string, bool) {
	// Resolve path: relative paths are prefixed with workspaceDir
	path := param.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceDir, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	// Trim whitespace from content
	return strings.TrimSpace(string(content)), true
}

// writeTerminationLog writes the output to /dev/termination-log.
// Kubernetes reads this file when the container terminates.
func writeTerminationLog(output terminationOutput) error {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	// Kubernetes termination log has a 4KB limit
	if len(data) > 4096 {
		return fmt.Errorf("output exceeds 4KB termination log limit (%d bytes)", len(data))
	}

	if err := os.WriteFile("/dev/termination-log", data, 0644); err != nil {
		return fmt.Errorf("failed to write termination log: %w", err)
	}

	return nil
}

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Note: getEnvOrDefault is defined in git_init.go and shared across commands
