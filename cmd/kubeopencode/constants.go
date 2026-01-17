// Copyright Contributors to the KubeOpenCode project

package main

// Common environment variable names shared across subcommands
const (
	envTaskName      = "TASK_NAME"
	envTaskNamespace = "TASK_NAMESPACE"
	envWorkspaceDir  = "WORKSPACE_DIR"
)

// Environment variable names for context-init
const (
	envConfigMapPath = "CONFIGMAP_PATH"
	envFileMappings  = "FILE_MAPPINGS"
	envDirMappings   = "DIR_MAPPINGS"
)
