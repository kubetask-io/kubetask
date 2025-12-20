// Copyright Contributors to the KubeTask project

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

// Environment variable names for save-session
const (
	envPVCMountPath = "PVC_MOUNT_PATH"
	envSignalFile   = "SIGNAL_FILE"
)
