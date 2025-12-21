// Copyright Contributors to the KubeTask project

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Note: Environment variable constants are defined in constants.go

// Default values for context-init
const (
	defaultWorkspaceDir  = "/workspace"
	defaultConfigMapPath = "/configmap-files"
)

// FileMapping represents a mapping from ConfigMap key to target file path
type FileMapping struct {
	Key        string `json:"key"`
	TargetPath string `json:"targetPath"`
	FileMode   *int32 `json:"fileMode,omitempty"` // Optional file permission mode (e.g., 0755)
}

// DirMapping represents a mapping from source directory to target directory
type DirMapping struct {
	SourcePath string `json:"sourcePath"`
	TargetPath string `json:"targetPath"`
}

func init() {
	rootCmd.AddCommand(contextInitCmd)
}

var contextInitCmd = &cobra.Command{
	Use:   "context-init",
	Short: "Copy ConfigMap content to workspace for writable access",
	Long: `context-init copies ConfigMap content to the workspace emptyDir volume.

This enables agents to create and modify files in the workspace directory,
which is not possible with direct ConfigMap mounts (read-only in Kubernetes).

Environment variables:
  WORKSPACE_DIR     Target workspace directory, default: /workspace
  CONFIGMAP_PATH    Path where ConfigMap is mounted, default: /configmap-files
  FILE_MAPPINGS     JSON array of file mappings: [{"key":"workspace-task.md","targetPath":"/workspace/task.md"}]
  DIR_MAPPINGS      JSON array of directory mappings: [{"sourcePath":"/configmap-dir-0","targetPath":"/workspace/guides"}]

Example:
  FILE_MAPPINGS='[{"key":"workspace-task.md","targetPath":"/workspace/task.md"}]'
  DIR_MAPPINGS='[{"sourcePath":"/configmap-dir-0","targetPath":"/workspace/guides"}]'
  /kubetask context-init`,
	RunE: runContextInit,
}

func runContextInit(cmd *cobra.Command, args []string) error {
	// Get configuration from environment variables
	workspaceDir := getEnvOrDefault(envWorkspaceDir, defaultWorkspaceDir)
	configMapPath := getEnvOrDefault(envConfigMapPath, defaultConfigMapPath)
	fileMappingsJSON := os.Getenv(envFileMappings)
	dirMappingsJSON := os.Getenv(envDirMappings)

	fmt.Println("context-init: Copying ConfigMap content to workspace...")
	fmt.Printf("  Workspace: %s\n", workspaceDir)
	fmt.Printf("  ConfigMap path: %s\n", configMapPath)

	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Parse and process file mappings
	if fileMappingsJSON != "" {
		var fileMappings []FileMapping
		if err := json.Unmarshal([]byte(fileMappingsJSON), &fileMappings); err != nil {
			return fmt.Errorf("failed to parse FILE_MAPPINGS: %w", err)
		}

		fmt.Printf("  File mappings: %d\n", len(fileMappings))
		for _, fm := range fileMappings {
			srcPath := filepath.Join(configMapPath, fm.Key)
			if err := copyFileWithMode(srcPath, fm.TargetPath, fm.FileMode); err != nil {
				// Log warning but continue - some files might be optional
				fmt.Printf("context-init: Warning: failed to copy %s to %s: %v\n", srcPath, fm.TargetPath, err)
			} else {
				modeStr := "0644"
				if fm.FileMode != nil {
					modeStr = fmt.Sprintf("%04o", *fm.FileMode)
				}
				fmt.Printf("context-init: Copied %s -> %s (mode: %s)\n", fm.Key, fm.TargetPath, modeStr)
			}
		}
	}

	// Parse and process directory mappings
	if dirMappingsJSON != "" {
		var dirMappings []DirMapping
		if err := json.Unmarshal([]byte(dirMappingsJSON), &dirMappings); err != nil {
			return fmt.Errorf("failed to parse DIR_MAPPINGS: %w", err)
		}

		fmt.Printf("  Directory mappings: %d\n", len(dirMappings))
		for _, dm := range dirMappings {
			if err := copyDir(dm.SourcePath, dm.TargetPath); err != nil {
				// Log warning but continue - some directories might be optional
				fmt.Printf("context-init: Warning: failed to copy directory %s to %s: %v\n", dm.SourcePath, dm.TargetPath, err)
			} else {
				fmt.Printf("context-init: Copied directory %s -> %s\n", dm.SourcePath, dm.TargetPath)
			}
		}
	}

	// Ensure all files in workspace are writable
	fmt.Println("context-init: Setting workspace permissions...")
	if err := makeWritable(workspaceDir); err != nil {
		fmt.Printf("context-init: Warning: could not set permissions: %v\n", err)
	}

	fmt.Println("context-init: Done!")
	return nil
}

// copyFile copies a file from src to dst, creating parent directories as needed
func copyFile(src, dst string) error {
	return copyFileWithMode(src, dst, nil)
}

// copyFileWithMode copies a file from src to dst with optional file mode
func copyFileWithMode(src, dst string, fileMode *int32) error {
	// Check if source file exists
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}
	if srcInfo.IsDir() {
		return fmt.Errorf("source is a directory, not a file")
	}

	// Create parent directory if needed
	dstDir := filepath.Dir(dst)
	if dstDir != "" && dstDir != "." {
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}
	}

	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Copy content
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy content: %w", err)
	}

	// Set permissions - use provided fileMode or default to 0644
	mode := os.FileMode(0644)
	if fileMode != nil {
		mode = os.FileMode(*fileMode)
	}
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	return nil
}

// copyDir recursively copies a directory from src to dst
func copyDir(src, dst string) error {
	// Check if source directory exists
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("source directory not found: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source is not a directory")
	}

	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Read source directory entries
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	// Copy each entry
	for _, entry := range entries {
		name := entry.Name()

		// Skip Kubernetes ConfigMap internal entries (..data, ..TIMESTAMP directories)
		// These are used for atomic updates and should not be copied directly
		if strings.HasPrefix(name, "..") {
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		// For symlinks (common with ConfigMap mounts), resolve to get the actual type
		info, err := os.Stat(srcPath) // Stat follows symlinks
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", srcPath, err)
		}

		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// makeWritable recursively sets write permissions on all files in a directory
func makeWritable(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip if path contains symlinks to avoid following them
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Get current mode and add write permission for owner
		mode := info.Mode()
		if info.IsDir() {
			// Directories need execute permission too
			newMode := mode | 0755
			if mode != newMode {
				if err := os.Chmod(path, newMode); err != nil {
					// Log but don't fail - some files might have restrictive permissions
					fmt.Printf("context-init: Warning: could not chmod %s: %v\n", path, err)
				}
			}
		} else {
			// Files just need read/write
			newMode := mode | 0644
			if mode != newMode {
				if err := os.Chmod(path, newMode); err != nil {
					fmt.Printf("context-init: Warning: could not chmod %s: %v\n", path, err)
				}
			}
		}

		return nil
	})
}
