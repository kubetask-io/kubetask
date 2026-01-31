// Copyright Contributors to the KubeOpenCode project

//go:build !integration

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

// defaultSystemConfig returns a systemConfig with default values for testing.
func defaultSystemConfig() systemConfig {
	return systemConfig{
		systemImage:           DefaultKubeOpenCodeImage,
		systemImagePullPolicy: corev1.PullIfNotPresent,
	}
}

func TestSanitizeConfigMapKey(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "simple path",
			filePath: "/workspace/task.md",
			want:     "workspace-task.md",
		},
		{
			name:     "nested path",
			filePath: "/workspace/guides/standards.md",
			want:     "workspace-guides-standards.md",
		},
		{
			name:     "deeply nested path",
			filePath: "/home/agent/.config/settings.json",
			want:     "home-agent-.config-settings.json",
		},
		{
			name:     "no leading slash",
			filePath: "workspace/task.md",
			want:     "workspace-task.md",
		},
		{
			name:     "single file",
			filePath: "/task.md",
			want:     "task.md",
		},
		{
			name:     "empty string",
			filePath: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeConfigMapKey(tt.filePath)
			if got != tt.want {
				t.Errorf("sanitizeConfigMapKey(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestBoolPtr(t *testing.T) {
	trueVal := boolPtr(true)
	if trueVal == nil || *trueVal != true {
		t.Errorf("boolPtr(true) = %v, want *true", trueVal)
	}

	falseVal := boolPtr(false)
	if falseVal == nil || *falseVal != false {
		t.Errorf("boolPtr(false) = %v, want *false", falseVal)
	}
}

func TestGetParentDir(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "simple path",
			filePath: "/etc/github-app/script.sh",
			want:     "/etc/github-app",
		},
		{
			name:     "nested path",
			filePath: "/workspace/guides/standards.md",
			want:     "/workspace/guides",
		},
		{
			name:     "root level file",
			filePath: "/task.md",
			want:     "/",
		},
		{
			name:     "single file without path",
			filePath: "task.md",
			want:     "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getParentDir(tt.filePath)
			if got != tt.want {
				t.Errorf("getParentDir(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestIsUnderPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		basePath string
		want     bool
	}{
		{
			name:     "file under workspace",
			filePath: "/workspace/task.md",
			basePath: "/workspace",
			want:     true,
		},
		{
			name:     "nested file under workspace",
			filePath: "/workspace/guides/readme.md",
			basePath: "/workspace",
			want:     true,
		},
		{
			name:     "file not under workspace",
			filePath: "/etc/github-app/script.sh",
			basePath: "/workspace",
			want:     false,
		},
		{
			name:     "similar prefix but different path",
			filePath: "/workspace-foo/task.md",
			basePath: "/workspace",
			want:     false,
		},
		{
			name:     "exact match",
			filePath: "/workspace",
			basePath: "/workspace",
			want:     true,
		},
		{
			name:     "basePath with trailing slash",
			filePath: "/workspace/task.md",
			basePath: "/workspace/",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnderPath(tt.filePath, tt.basePath)
			if got != tt.want {
				t.Errorf("isUnderPath(%q, %q) = %v, want %v", tt.filePath, tt.basePath, got, tt.want)
			}
		})
	}
}

func TestSanitizeVolumeName(t *testing.T) {
	tests := []struct {
		name    string
		dirPath string
		want    string
	}{
		{
			name:    "simple path",
			dirPath: "/etc/github-app",
			want:    "ctx-etc-github-app",
		},
		{
			name:    "nested path",
			dirPath: "/home/user/.config",
			want:    "ctx-home-user-.config",
		},
		{
			name:    "uppercase path",
			dirPath: "/ETC/Config",
			want:    "ctx-etc-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeVolumeName(tt.dirPath)
			if got != tt.want {
				t.Errorf("sanitizeVolumeName(%q) = %q, want %q", tt.dirPath, got, tt.want)
			}
		})
	}
}

func TestBuildPod_BasicTask(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	// Verify pod metadata
	if pod.Name != "test-task-pod" {
		t.Errorf("Pod.Name = %q, want %q", pod.Name, "test-task-pod")
	}
	if pod.Namespace != "default" {
		t.Errorf("Pod.Namespace = %q, want %q", pod.Namespace, "default")
	}

	// Verify labels
	if pod.Labels["app"] != "kubeopencode" {
		t.Errorf("Pod.Labels[app] = %q, want %q", pod.Labels["app"], "kubeopencode")
	}
	if pod.Labels["kubeopencode.io/task"] != "test-task" {
		t.Errorf("Pod.Labels[kubeopencode.io/task] = %q, want %q", pod.Labels["kubeopencode.io/task"], "test-task")
	}

	// Verify no owner reference (cleanup is handled via finalizer)
	if len(pod.OwnerReferences) != 0 {
		t.Fatalf("len(Pod.OwnerReferences) = %d, want 0 (cleanup via finalizer)", len(pod.OwnerReferences))
	}

	// Verify init containers (OpenCode init should be first)
	if len(pod.Spec.InitContainers) < 1 {
		t.Fatalf("len(InitContainers) = %d, want at least 1", len(pod.Spec.InitContainers))
	}
	initContainer := pod.Spec.InitContainers[0]
	if initContainer.Name != "opencode-init" {
		t.Errorf("InitContainer.Name = %q, want %q", initContainer.Name, "opencode-init")
	}
	if initContainer.Image != "test-opencode:v1.0.0" {
		t.Errorf("InitContainer.Image = %q, want %q", initContainer.Image, "test-opencode:v1.0.0")
	}

	// Verify init container mounts /tools volume
	var initHasToolsMount bool
	for _, vm := range initContainer.VolumeMounts {
		if vm.Name == "tools" && vm.MountPath == "/tools" {
			initHasToolsMount = true
			break
		}
	}
	if !initHasToolsMount {
		t.Errorf("InitContainer should mount /tools volume")
	}

	// Verify container (uses executorImage)
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1", len(pod.Spec.Containers))
	}
	container := pod.Spec.Containers[0]
	if container.Name != "agent" {
		t.Errorf("Container.Name = %q, want %q", container.Name, "agent")
	}
	if container.Image != "test-executor:v1.0.0" {
		t.Errorf("Container.Image = %q, want %q", container.Image, "test-executor:v1.0.0")
	}

	// Verify container mounts /tools volume
	var containerHasToolsMount bool
	for _, vm := range container.VolumeMounts {
		if vm.Name == "tools" && vm.MountPath == "/tools" {
			containerHasToolsMount = true
			break
		}
	}
	if !containerHasToolsMount {
		t.Errorf("Container should mount /tools volume")
	}

	// Verify environment variables
	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}
	if envMap["TASK_NAME"] != "test-task" {
		t.Errorf("Env[TASK_NAME] = %q, want %q", envMap["TASK_NAME"], "test-task")
	}
	if envMap["TASK_NAMESPACE"] != "default" {
		t.Errorf("Env[TASK_NAMESPACE] = %q, want %q", envMap["TASK_NAMESPACE"], "default")
	}
	if envMap["WORKSPACE_DIR"] != "/workspace" {
		t.Errorf("Env[WORKSPACE_DIR] = %q, want %q", envMap["WORKSPACE_DIR"], "/workspace")
	}

	// Verify service account
	if pod.Spec.ServiceAccountName != "test-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", pod.Spec.ServiceAccountName, "test-sa")
	}

	// Verify restart policy
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want %q", pod.Spec.RestartPolicy, corev1.RestartPolicyNever)
	}
}

// stringPtr returns a pointer to the given string value
func stringPtr(s string) *string {
	return &s
}

func TestBuildPod_WithCredentials(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	envName := "API_TOKEN"
	mountPath := "/home/agent/.ssh/id_rsa"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		credentials: []kubeopenv1alpha1.Credential{
			{
				Name: "api-token",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "my-secret",
					Key:  stringPtr("token"),
				},
				Env: &envName,
			},
			{
				Name: "ssh-key",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "ssh-secret",
					Key:  stringPtr("private-key"),
				},
				MountPath: &mountPath,
			},
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	container := pod.Spec.Containers[0]

	// Verify env credential
	var foundEnvCred bool
	for _, env := range container.Env {
		if env.Name == "API_TOKEN" {
			foundEnvCred = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Errorf("API_TOKEN env should have SecretKeyRef")
			} else {
				if env.ValueFrom.SecretKeyRef.Name != "my-secret" {
					t.Errorf("SecretKeyRef.Name = %q, want %q", env.ValueFrom.SecretKeyRef.Name, "my-secret")
				}
				if env.ValueFrom.SecretKeyRef.Key != "token" {
					t.Errorf("SecretKeyRef.Key = %q, want %q", env.ValueFrom.SecretKeyRef.Key, "token")
				}
			}
		}
	}
	if !foundEnvCred {
		t.Errorf("API_TOKEN env not found")
	}

	// Verify mount credential
	var foundMountCred bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/home/agent/.ssh/id_rsa" {
			foundMountCred = true
		}
	}
	if !foundMountCred {
		t.Errorf("SSH key mount not found at /home/agent/.ssh/id_rsa")
	}

	// Verify volume exists
	var foundVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Secret != nil && vol.Secret.SecretName == "ssh-secret" {
			foundVolume = true
		}
	}
	if !foundVolume {
		t.Errorf("Secret volume for ssh-secret not found")
	}
}

func TestBuildPod_WithEntireSecretCredential(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		credentials: []kubeopenv1alpha1.Credential{
			{
				// No Key specified - mount entire secret as env vars
				Name: "api-keys",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "api-credentials",
					// Key is nil - entire secret should be mounted
				},
			},
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	container := pod.Spec.Containers[0]

	// Verify envFrom is set with secretRef
	if len(container.EnvFrom) != 1 {
		t.Fatalf("Expected 1 envFrom entry, got %d", len(container.EnvFrom))
	}

	envFrom := container.EnvFrom[0]
	if envFrom.SecretRef == nil {
		t.Errorf("EnvFrom.SecretRef should not be nil")
	} else if envFrom.SecretRef.Name != "api-credentials" {
		t.Errorf("EnvFrom.SecretRef.Name = %q, want %q", envFrom.SecretRef.Name, "api-credentials")
	}
}

func TestBuildPod_WithMixedCredentials(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	envName := "GITHUB_TOKEN"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		credentials: []kubeopenv1alpha1.Credential{
			{
				// Entire secret mount (no key)
				Name: "all-api-keys",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "api-credentials",
				},
			},
			{
				// Single key mount with env rename
				Name: "github-token",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "github-secret",
					Key:  stringPtr("token"),
				},
				Env: &envName,
			},
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	container := pod.Spec.Containers[0]

	// Verify envFrom has 1 entry (entire secret)
	if len(container.EnvFrom) != 1 {
		t.Fatalf("Expected 1 envFrom entry, got %d", len(container.EnvFrom))
	}
	if container.EnvFrom[0].SecretRef.Name != "api-credentials" {
		t.Errorf("EnvFrom.SecretRef.Name = %q, want %q", container.EnvFrom[0].SecretRef.Name, "api-credentials")
	}

	// Verify env has GITHUB_TOKEN from single key mount
	var foundGithubToken bool
	for _, env := range container.Env {
		if env.Name == "GITHUB_TOKEN" {
			foundGithubToken = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Errorf("GITHUB_TOKEN env should have SecretKeyRef")
			} else {
				if env.ValueFrom.SecretKeyRef.Name != "github-secret" {
					t.Errorf("SecretKeyRef.Name = %q, want %q", env.ValueFrom.SecretKeyRef.Name, "github-secret")
				}
				if env.ValueFrom.SecretKeyRef.Key != "token" {
					t.Errorf("SecretKeyRef.Key = %q, want %q", env.ValueFrom.SecretKeyRef.Key, "token")
				}
			}
		}
	}
	if !foundGithubToken {
		t.Errorf("GITHUB_TOKEN env not found")
	}
}

func TestBuildPod_WithEntireSecretAsDirectory(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	mountPath := "/etc/ssl/certs"
	var fileMode int32 = 0400

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		credentials: []kubeopenv1alpha1.Credential{
			{
				// No Key specified + MountPath = mount entire secret as directory
				Name: "tls-certs",
				SecretRef: kubeopenv1alpha1.SecretReference{
					Name: "tls-certificates",
					// Key is nil - entire secret should be mounted as directory
				},
				MountPath: &mountPath,
				FileMode:  &fileMode,
			},
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	container := pod.Spec.Containers[0]

	// Verify envFrom is NOT set (should not be env vars)
	if len(container.EnvFrom) != 0 {
		t.Errorf("Expected 0 envFrom entries, got %d", len(container.EnvFrom))
	}

	// Verify volume is created
	var foundVolume bool
	var volumeName string
	for _, vol := range pod.Spec.Volumes {
		if vol.Secret != nil && vol.Secret.SecretName == "tls-certificates" {
			foundVolume = true
			volumeName = vol.Name

			// Verify DefaultMode is set
			if vol.Secret.DefaultMode == nil {
				t.Errorf("Expected DefaultMode to be set")
			} else if *vol.Secret.DefaultMode != fileMode {
				t.Errorf("DefaultMode = %d, want %d", *vol.Secret.DefaultMode, fileMode)
			}

			// Verify Items is NOT set (mounting entire secret)
			if len(vol.Secret.Items) != 0 {
				t.Errorf("Expected no Items for entire secret mount, got %d", len(vol.Secret.Items))
			}
			break
		}
	}
	if !foundVolume {
		t.Fatalf("Volume for tls-certificates secret not found")
	}

	// Verify volumeMount is created
	var foundVolumeMount bool
	for _, vm := range container.VolumeMounts {
		if vm.Name == volumeName && vm.MountPath == mountPath {
			foundVolumeMount = true

			// Verify SubPath is NOT set (mounting entire directory)
			if vm.SubPath != "" {
				t.Errorf("SubPath should be empty for directory mount, got %q", vm.SubPath)
			}
			break
		}
	}
	if !foundVolumeMount {
		t.Errorf("VolumeMount for %s not found", mountPath)
	}
}

func TestBuildPod_WithPodScheduling(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	runtimeClass := "gvisor"
	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		podSpec: &kubeopenv1alpha1.AgentPodSpec{
			Labels: map[string]string{
				"custom-label": "custom-value",
			},
			Scheduling: &kubeopenv1alpha1.PodScheduling{
				NodeSelector: map[string]string{
					"node-type": "gpu",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "dedicated",
						Operator: corev1.TolerationOpEqual,
						Value:    "ai-workload",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			RuntimeClassName: &runtimeClass,
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	// Verify node selector
	if pod.Spec.NodeSelector["node-type"] != "gpu" {
		t.Errorf("NodeSelector[node-type] = %q, want %q", pod.Spec.NodeSelector["node-type"], "gpu")
	}

	// Verify tolerations
	if len(pod.Spec.Tolerations) != 1 {
		t.Fatalf("len(Tolerations) = %d, want 1", len(pod.Spec.Tolerations))
	}
	if pod.Spec.Tolerations[0].Key != "dedicated" {
		t.Errorf("Tolerations[0].Key = %q, want %q", pod.Spec.Tolerations[0].Key, "dedicated")
	}

	// Verify runtime class
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "gvisor" {
		t.Errorf("RuntimeClassName = %v, want %q", pod.Spec.RuntimeClassName, "gvisor")
	}

	// Verify custom label on pod
	if pod.Labels["custom-label"] != "custom-value" {
		t.Errorf("Pod.Labels[custom-label] = %q, want %q", pod.Labels["custom-label"], "custom-value")
	}
	// Verify base labels are still present
	if pod.Labels["app"] != "kubeopencode" {
		t.Errorf("Pod.Labels[app] = %q, want %q", pod.Labels["app"], "kubeopencode")
	}
}

func TestBuildPod_WithContextConfigMap(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	contextConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-context",
			Namespace: "default",
		},
		Data: map[string]string{
			"workspace-task.md": "# Test Task",
		},
	}

	fileMounts := []fileMount{
		{filePath: "/workspace/task.md"},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, contextConfigMap, fileMounts, nil, nil, defaultSystemConfig(), "")

	// Verify context-files volume exists (for init container to read from)
	var foundContextVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "context-files" && vol.ConfigMap != nil {
			foundContextVolume = true
			if vol.ConfigMap.Name != "test-task-context" {
				t.Errorf("context-files volume ConfigMap.Name = %q, want %q", vol.ConfigMap.Name, "test-task-context")
			}
		}
	}
	if !foundContextVolume {
		t.Errorf("context-files volume not found")
	}

	// Verify workspace emptyDir volume exists
	var foundWorkspaceVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == WorkspaceVolumeName && vol.EmptyDir != nil {
			foundWorkspaceVolume = true
		}
	}
	if !foundWorkspaceVolume {
		t.Errorf("workspace emptyDir volume not found")
	}

	// Verify agent container mounts workspace emptyDir
	container := pod.Spec.Containers[0]
	var foundWorkspaceMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace" && mount.Name == WorkspaceVolumeName {
			foundWorkspaceMount = true
		}
	}
	if !foundWorkspaceMount {
		t.Errorf("Agent container should mount workspace emptyDir at /workspace")
	}

	// Verify context-init container exists
	initContainers := pod.Spec.InitContainers
	var foundContextInit bool
	for _, ic := range initContainers {
		if ic.Name == "context-init" {
			foundContextInit = true
			// Verify init container mounts the ConfigMap
			var foundConfigMapMount bool
			var foundInitWorkspaceMount bool
			for _, mount := range ic.VolumeMounts {
				if mount.Name == "context-files" && mount.MountPath == "/configmap-files" {
					foundConfigMapMount = true
				}
				if mount.Name == WorkspaceVolumeName && mount.MountPath == "/workspace" {
					foundInitWorkspaceMount = true
				}
			}
			if !foundConfigMapMount {
				t.Errorf("context-init container should mount context-files ConfigMap at /configmap-files")
			}
			if !foundInitWorkspaceMount {
				t.Errorf("context-init container should mount workspace emptyDir at /workspace")
			}
		}
	}
	if !foundContextInit {
		t.Errorf("context-init init container not found")
	}
}

func TestBuildPod_WithDirMounts(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	dirMounts := []dirMount{
		{
			dirPath:       "/workspace/guides",
			configMapName: "guides-configmap",
			optional:      true,
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, dirMounts, nil, defaultSystemConfig(), "")

	// Verify dir-mount volume exists (for init container to read from)
	var foundDirVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "dir-mount-0" && vol.ConfigMap != nil {
			foundDirVolume = true
			if vol.ConfigMap.Name != "guides-configmap" {
				t.Errorf("dir-mount-0 volume ConfigMap.Name = %q, want %q", vol.ConfigMap.Name, "guides-configmap")
			}
			if vol.ConfigMap.Optional == nil || *vol.ConfigMap.Optional != true {
				t.Errorf("dir-mount-0 volume ConfigMap.Optional = %v, want true", vol.ConfigMap.Optional)
			}
		}
	}
	if !foundDirVolume {
		t.Errorf("dir-mount-0 volume not found")
	}

	// Verify workspace emptyDir volume exists
	var foundWorkspaceVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == WorkspaceVolumeName && vol.EmptyDir != nil {
			foundWorkspaceVolume = true
		}
	}
	if !foundWorkspaceVolume {
		t.Errorf("workspace emptyDir volume not found")
	}

	// Verify agent container mounts workspace emptyDir (not dir-mount directly)
	container := pod.Spec.Containers[0]
	var foundWorkspaceMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace" && mount.Name == WorkspaceVolumeName {
			foundWorkspaceMount = true
		}
	}
	if !foundWorkspaceMount {
		t.Errorf("Agent container should mount workspace emptyDir at /workspace")
	}

	// Verify context-init container exists and mounts the ConfigMap
	initContainers := pod.Spec.InitContainers
	var foundContextInit bool
	for _, ic := range initContainers {
		if ic.Name == "context-init" {
			foundContextInit = true
			// Verify init container mounts the dir-mount ConfigMap
			var foundDirMount bool
			for _, mount := range ic.VolumeMounts {
				if mount.Name == "dir-mount-0" && mount.MountPath == "/configmap-dir-0" {
					foundDirMount = true
				}
			}
			if !foundDirMount {
				t.Errorf("context-init container should mount dir-mount-0 ConfigMap at /configmap-dir-0")
			}
		}
	}
	if !foundContextInit {
		t.Errorf("context-init init container not found")
	}
}

func TestBuildPod_WithGitMounts(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubeopencode.io/v1alpha1",
			Kind:       "Task",
		},
	}

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	gitMounts := []gitMount{
		{
			contextName: "my-context",
			repository:  "https://github.com/org/repo.git",
			ref:         "main",
			repoPath:    ".claude/",
			mountPath:   "/workspace/.claude",
			depth:       1,
			secretName:  "",
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, gitMounts, defaultSystemConfig(), "")

	// Verify init containers exist (opencode-init first, then git-init-0)
	if len(pod.Spec.InitContainers) != 2 {
		t.Fatalf("Expected 2 init containers (opencode-init + git-init), got %d", len(pod.Spec.InitContainers))
	}

	// First init container should be opencode-init
	openCodeInit := pod.Spec.InitContainers[0]
	if openCodeInit.Name != "opencode-init" {
		t.Errorf("First init container name = %q, want %q", openCodeInit.Name, "opencode-init")
	}

	// Second init container should be git-init-0
	initContainer := pod.Spec.InitContainers[1]
	if initContainer.Name != "git-init-0" {
		t.Errorf("Git init container name = %q, want %q", initContainer.Name, "git-init-0")
	}
	if initContainer.Image != DefaultKubeOpenCodeImage {
		t.Errorf("Git init container image = %q, want %q", initContainer.Image, DefaultKubeOpenCodeImage)
	}

	// Verify environment variables
	envMap := make(map[string]string)
	for _, env := range initContainer.Env {
		envMap[env.Name] = env.Value
	}
	if envMap["GIT_REPO"] != "https://github.com/org/repo.git" {
		t.Errorf("GIT_REPO = %q, want %q", envMap["GIT_REPO"], "https://github.com/org/repo.git")
	}
	if envMap["GIT_REF"] != "main" {
		t.Errorf("GIT_REF = %q, want %q", envMap["GIT_REF"], "main")
	}
	if envMap["GIT_DEPTH"] != "1" {
		t.Errorf("GIT_DEPTH = %q, want %q", envMap["GIT_DEPTH"], "1")
	}

	// Verify emptyDir volume exists
	var foundGitVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "git-context-0" && vol.EmptyDir != nil {
			foundGitVolume = true
		}
	}
	if !foundGitVolume {
		t.Errorf("git-context-0 emptyDir volume not found")
	}

	// Verify volume mount in agent container with correct subPath
	container := pod.Spec.Containers[0]
	var foundMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace/.claude" && mount.Name == "git-context-0" {
			foundMount = true
			expectedSubPath := "repo/.claude/"
			if mount.SubPath != expectedSubPath {
				t.Errorf("Volume mount SubPath = %q, want %q", mount.SubPath, expectedSubPath)
			}
		}
	}
	if !foundMount {
		t.Errorf("Volume mount for /workspace/.claude not found")
	}
}

func TestBuildPod_WithGitMountsAndAuth(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubeopencode.io/v1alpha1",
			Kind:       "Task",
		},
	}

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	gitMounts := []gitMount{
		{
			contextName: "private-repo",
			repository:  "https://github.com/org/private-repo.git",
			ref:         "v1.0.0",
			repoPath:    "",
			mountPath:   "/workspace/git-private-repo",
			depth:       1,
			secretName:  "git-credentials",
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, gitMounts, defaultSystemConfig(), "")

	// Verify we have 2 init containers (opencode-init + git-init)
	if len(pod.Spec.InitContainers) != 2 {
		t.Fatalf("Expected 2 init containers, got %d", len(pod.Spec.InitContainers))
	}

	// Verify git-init container (second one) has auth env vars
	gitInitContainer := pod.Spec.InitContainers[1]
	if gitInitContainer.Name != "git-init-0" {
		t.Errorf("Second init container name = %q, want %q", gitInitContainer.Name, "git-init-0")
	}

	var foundUsername, foundPassword bool
	for _, env := range gitInitContainer.Env {
		if env.Name == "GIT_USERNAME" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			if env.ValueFrom.SecretKeyRef.Name == "git-credentials" && env.ValueFrom.SecretKeyRef.Key == "username" {
				foundUsername = true
			}
		}
		if env.Name == "GIT_PASSWORD" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			if env.ValueFrom.SecretKeyRef.Name == "git-credentials" && env.ValueFrom.SecretKeyRef.Key == "password" {
				foundPassword = true
			}
		}
	}
	if !foundUsername {
		t.Errorf("GIT_USERNAME env var with secret reference not found")
	}
	if !foundPassword {
		t.Errorf("GIT_PASSWORD env var with secret reference not found")
	}

	// Verify volume mount without subPath (entire repo)
	container := pod.Spec.Containers[0]
	var foundMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace/git-private-repo" && mount.Name == "git-context-0" {
			foundMount = true
			if mount.SubPath != "repo" {
				t.Errorf("Volume mount SubPath = %q, want %q", mount.SubPath, "repo")
			}
		}
	}
	if !foundMount {
		t.Errorf("Volume mount for /workspace/git-private-repo not found")
	}
}

func TestBuildGitInitContainer(t *testing.T) {
	gm := gitMount{
		contextName: "test-context",
		repository:  "https://github.com/test/repo.git",
		ref:         "develop",
		repoPath:    "docs/",
		mountPath:   "/workspace/docs",
		depth:       5,
		secretName:  "",
	}

	container := buildGitInitContainer(gm, "git-vol-0", 0, defaultSystemConfig())

	if container.Name != "git-init-0" {
		t.Errorf("Container name = %q, want %q", container.Name, "git-init-0")
	}

	if container.Image != DefaultKubeOpenCodeImage {
		t.Errorf("Container image = %q, want %q", container.Image, DefaultKubeOpenCodeImage)
	}

	// Check env vars
	envMap := make(map[string]string)
	for _, env := range container.Env {
		if env.Value != "" {
			envMap[env.Name] = env.Value
		}
	}

	if envMap["GIT_REPO"] != "https://github.com/test/repo.git" {
		t.Errorf("GIT_REPO = %q, want %q", envMap["GIT_REPO"], "https://github.com/test/repo.git")
	}
	if envMap["GIT_REF"] != "develop" {
		t.Errorf("GIT_REF = %q, want %q", envMap["GIT_REF"], "develop")
	}
	if envMap["GIT_DEPTH"] != "5" {
		t.Errorf("GIT_DEPTH = %q, want %q", envMap["GIT_DEPTH"], "5")
	}

	// Verify volume mount
	if len(container.VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(container.VolumeMounts))
	}
	if container.VolumeMounts[0].Name != "git-vol-0" {
		t.Errorf("Volume mount name = %q, want %q", container.VolumeMounts[0].Name, "git-vol-0")
	}
	if container.VolumeMounts[0].MountPath != "/git" {
		t.Errorf("Volume mount path = %q, want %q", container.VolumeMounts[0].MountPath, "/git")
	}
}

func TestBuildContextInitContainer(t *testing.T) {
	tests := []struct {
		name         string
		workspaceDir string
		fileMounts   []fileMount
		dirMounts    []dirMount
		wantEnvVars  map[string]string
	}{
		{
			name:         "with file mounts only",
			workspaceDir: "/workspace",
			fileMounts: []fileMount{
				{filePath: "/workspace/task.md"},
				{filePath: "/workspace/guides/readme.md"},
			},
			dirMounts: nil,
			wantEnvVars: map[string]string{
				"WORKSPACE_DIR":  "/workspace",
				"CONFIGMAP_PATH": "/configmap-files",
				"FILE_MAPPINGS":  `[{"key":"workspace-task.md","targetPath":"/workspace/task.md"},{"key":"workspace-guides-readme.md","targetPath":"/workspace/guides/readme.md"}]`,
			},
		},
		{
			name:         "with dir mounts only",
			workspaceDir: "/workspace",
			fileMounts:   nil,
			dirMounts: []dirMount{
				{dirPath: "/workspace/config", configMapName: "config-cm"},
				{dirPath: "/workspace/scripts", configMapName: "scripts-cm"},
			},
			wantEnvVars: map[string]string{
				"WORKSPACE_DIR":  "/workspace",
				"CONFIGMAP_PATH": "/configmap-files",
				"DIR_MAPPINGS":   `[{"sourcePath":"/configmap-dir-0","targetPath":"/workspace/config"},{"sourcePath":"/configmap-dir-1","targetPath":"/workspace/scripts"}]`,
			},
		},
		{
			name:         "with both file and dir mounts",
			workspaceDir: "/workspace",
			fileMounts: []fileMount{
				{filePath: "/workspace/task.md"},
			},
			dirMounts: []dirMount{
				{dirPath: "/workspace/guides", configMapName: "guides-cm"},
			},
			wantEnvVars: map[string]string{
				"WORKSPACE_DIR":  "/workspace",
				"CONFIGMAP_PATH": "/configmap-files",
				"FILE_MAPPINGS":  `[{"key":"workspace-task.md","targetPath":"/workspace/task.md"}]`,
				"DIR_MAPPINGS":   `[{"sourcePath":"/configmap-dir-0","targetPath":"/workspace/guides"}]`,
			},
		},
		{
			name:         "with custom workspace dir",
			workspaceDir: "/home/agent/work",
			fileMounts: []fileMount{
				{filePath: "/home/agent/work/task.md"},
			},
			dirMounts: nil,
			wantEnvVars: map[string]string{
				"WORKSPACE_DIR":  "/home/agent/work",
				"CONFIGMAP_PATH": "/configmap-files",
				"FILE_MAPPINGS":  `[{"key":"home-agent-work-task.md","targetPath":"/home/agent/work/task.md"}]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := buildContextInitContainer(tt.workspaceDir, tt.fileMounts, tt.dirMounts, defaultSystemConfig())

			// Verify container name
			if container.Name != "context-init" {
				t.Errorf("Container.Name = %q, want %q", container.Name, "context-init")
			}

			// Verify image
			if container.Image != DefaultKubeOpenCodeImage {
				t.Errorf("Container.Image = %q, want %q", container.Image, DefaultKubeOpenCodeImage)
			}

			// Verify command uses /kubeopencode context-init
			if len(container.Command) != 2 {
				t.Fatalf("len(Container.Command) = %d, want 2", len(container.Command))
			}
			if container.Command[0] != "/kubeopencode" {
				t.Errorf("Container.Command[0] = %q, want %q", container.Command[0], "/kubeopencode")
			}
			if container.Command[1] != "context-init" {
				t.Errorf("Container.Command[1] = %q, want %q", container.Command[1], "context-init")
			}

			// Verify environment variables
			envMap := make(map[string]string)
			for _, env := range container.Env {
				envMap[env.Name] = env.Value
			}

			for key, wantValue := range tt.wantEnvVars {
				gotValue, ok := envMap[key]
				if !ok {
					t.Errorf("Missing expected env var: %s", key)
					continue
				}
				if gotValue != wantValue {
					t.Errorf("Env[%s] = %q, want %q", key, gotValue, wantValue)
				}
			}

			// Verify no unexpected env vars for FILE_MAPPINGS/DIR_MAPPINGS
			if len(tt.fileMounts) == 0 {
				if _, ok := envMap["FILE_MAPPINGS"]; ok {
					t.Errorf("FILE_MAPPINGS should not be set when there are no file mounts")
				}
			}
			if len(tt.dirMounts) == 0 {
				if _, ok := envMap["DIR_MAPPINGS"]; ok {
					t.Errorf("DIR_MAPPINGS should not be set when there are no dir mounts")
				}
			}
		})
	}
}

// TestBuildPod_WithExternalFileMounts tests that files mounted outside of /workspace
// are properly handled by creating shared emptyDir volumes.
func TestBuildPod_WithExternalFileMounts(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	contextConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-context",
			Namespace: "default",
		},
		Data: map[string]string{
			"workspace-task.md":                "# Test Task",
			"etc-github-app-github-app-iat.sh": "#!/bin/bash\necho token",
		},
	}

	// File mounts: one inside /workspace, one outside (in /etc/github-app)
	fileMounts := []fileMount{
		{filePath: "/workspace/task.md"},
		{filePath: "/etc/github-app/github-app-iat.sh"},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, contextConfigMap, fileMounts, nil, nil, defaultSystemConfig(), "")

	// Verify workspace emptyDir volume exists
	var foundWorkspaceVolume bool
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == WorkspaceVolumeName && vol.EmptyDir != nil {
			foundWorkspaceVolume = true
		}
	}
	if !foundWorkspaceVolume {
		t.Errorf("workspace volume not found")
	}

	// Verify external directory volume exists (for /etc/github-app)
	var foundExternalVolume bool
	var externalVolumeName string
	for _, vol := range pod.Spec.Volumes {
		if strings.HasPrefix(vol.Name, "ctx-") && vol.EmptyDir != nil {
			foundExternalVolume = true
			externalVolumeName = vol.Name
		}
	}
	if !foundExternalVolume {
		t.Errorf("external directory volume (ctx-*) not found for /etc/github-app")
	}

	// Verify context-init container has the external volume mounted
	var contextInitContainer *corev1.Container
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == "context-init" {
			contextInitContainer = &pod.Spec.InitContainers[i]
			break
		}
	}
	if contextInitContainer == nil {
		t.Fatalf("context-init container not found")
	}

	var contextInitHasExternalMount bool
	for _, vm := range contextInitContainer.VolumeMounts {
		if vm.Name == externalVolumeName && vm.MountPath == "/etc/github-app" {
			contextInitHasExternalMount = true
		}
	}
	if !contextInitHasExternalMount {
		t.Errorf("context-init container does not have external volume mounted at /etc/github-app")
	}

	// Verify agent container has the external volume mounted
	var agentContainer *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "agent" {
			agentContainer = &pod.Spec.Containers[i]
			break
		}
	}
	if agentContainer == nil {
		t.Fatalf("agent container not found")
	}

	var agentHasExternalMount bool
	for _, vm := range agentContainer.VolumeMounts {
		if vm.Name == externalVolumeName && vm.MountPath == "/etc/github-app" {
			agentHasExternalMount = true
		}
	}
	if !agentHasExternalMount {
		t.Errorf("agent container does not have external volume mounted at /etc/github-app")
	}
}

// TestResolveMountPath tests the Tekton-style path resolution for mountPath.
// Paths starting with "/" are absolute, paths without "/" prefix are relative
// and get prefixed with workspaceDir.
func TestResolveMountPath(t *testing.T) {
	tests := []struct {
		name         string
		mountPath    string
		workspaceDir string
		want         string
	}{
		{
			name:         "empty path returns empty",
			mountPath:    "",
			workspaceDir: "/workspace",
			want:         "",
		},
		{
			name:         "absolute path unchanged",
			mountPath:    "/etc/config/app.conf",
			workspaceDir: "/workspace",
			want:         "/etc/config/app.conf",
		},
		{
			name:         "absolute path with workspace prefix unchanged",
			mountPath:    "/workspace/task.md",
			workspaceDir: "/workspace",
			want:         "/workspace/task.md",
		},
		{
			name:         "relative path gets prefixed",
			mountPath:    "guides/readme.md",
			workspaceDir: "/workspace",
			want:         "/workspace/guides/readme.md",
		},
		{
			name:         "simple filename gets prefixed",
			mountPath:    "task-context.md",
			workspaceDir: "/workspace",
			want:         "/workspace/task-context.md",
		},
		{
			name:         "dot-slash relative path gets prefixed",
			mountPath:    "./guides/readme.md",
			workspaceDir: "/workspace",
			want:         "/workspace/./guides/readme.md",
		},
		{
			name:         "relative path with custom workspaceDir",
			mountPath:    "config/settings.yaml",
			workspaceDir: "/home/agent",
			want:         "/home/agent/config/settings.yaml",
		},
		{
			name:         "deeply nested relative path",
			mountPath:    "a/b/c/d/file.txt",
			workspaceDir: "/workspace",
			want:         "/workspace/a/b/c/d/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMountPath(tt.mountPath, tt.workspaceDir)
			if got != tt.want {
				t.Errorf("resolveMountPath(%q, %q) = %q, want %q", tt.mountPath, tt.workspaceDir, got, tt.want)
			}
		})
	}
}

func TestBuildPod_WithOpenCodeConfig(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	configJSON := `{"model": "google/gemini-2.5-pro", "small_model": "google/gemini-2.5-flash"}`
	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		config:             &configJSON,
	}

	// Create a ConfigMap with the config content
	// Use sanitizeConfigMapKey to match the key generated by task_controller
	expectedConfigKey := sanitizeConfigMapKey(OpenCodeConfigPath)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-context",
			Namespace: "default",
		},
		Data: map[string]string{
			expectedConfigKey: configJSON,
		},
	}

	// Include the config file in fileMounts
	fileMounts := []fileMount{
		{filePath: OpenCodeConfigPath},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, configMap, fileMounts, nil, nil, defaultSystemConfig(), "")

	// Verify OPENCODE_CONFIG env var is set
	container := pod.Spec.Containers[0]
	var foundOpenCodeConfigEnv bool
	for _, env := range container.Env {
		if env.Name == OpenCodeConfigEnvVar {
			if env.Value != OpenCodeConfigPath {
				t.Errorf("OPENCODE_CONFIG env value = %q, want %q", env.Value, OpenCodeConfigPath)
			}
			foundOpenCodeConfigEnv = true
			break
		}
	}
	if !foundOpenCodeConfigEnv {
		t.Errorf("OPENCODE_CONFIG env var not found in container env")
	}

	// Verify context-init container has /tools volume mount
	var contextInitContainer *corev1.Container
	for i, initC := range pod.Spec.InitContainers {
		if initC.Name == "context-init" {
			contextInitContainer = &pod.Spec.InitContainers[i]
			break
		}
	}
	if contextInitContainer == nil {
		t.Fatalf("context-init container not found")
	}

	var hasToolsMount bool
	for _, vm := range contextInitContainer.VolumeMounts {
		if vm.Name == ToolsVolumeName && vm.MountPath == ToolsMountPath {
			hasToolsMount = true
			break
		}
	}
	if !hasToolsMount {
		t.Errorf("context-init container should mount /tools volume for config file")
	}
}

func TestBuildPod_WithoutOpenCodeConfig(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		config:             nil, // No config provided
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, nil, nil, nil, defaultSystemConfig(), "")

	// Verify OPENCODE_CONFIG env var is NOT set
	container := pod.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == OpenCodeConfigEnvVar {
			t.Errorf("OPENCODE_CONFIG env var should not be set when config is nil")
		}
	}
}

func TestBuildPod_WithContextFile(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	// Create context file mount (simulates context without mountPath)
	contextFilePath := cfg.workspaceDir + "/" + ContextFileRelPath
	fileMounts := []fileMount{
		{filePath: contextFilePath},
	}

	contextConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-context",
			Namespace: "default",
		},
		Data: map[string]string{
			sanitizeConfigMapKey(contextFilePath): "<context>test content</context>",
		},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, contextConfigMap, fileMounts, nil, nil, defaultSystemConfig(), "")

	// Verify OPENCODE_CONFIG_CONTENT env var is set
	container := pod.Spec.Containers[0]
	foundConfigContentEnv := false
	for _, env := range container.Env {
		if env.Name == OpenCodeConfigContentEnvVar {
			foundConfigContentEnv = true
			expectedValue := `{"instructions":["` + ContextFileRelPath + `"]}`
			if env.Value != expectedValue {
				t.Errorf("OPENCODE_CONFIG_CONTENT env value = %q, want %q", env.Value, expectedValue)
			}
			break
		}
	}
	if !foundConfigContentEnv {
		t.Errorf("OPENCODE_CONFIG_CONTENT env var not found in container env")
	}
}

func TestBuildPod_WithoutContextFile(t *testing.T) {
	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubeopencode.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-opencode:v1.0.0",
		executorImage:      "test-executor:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
	}

	// No context file mount - only task.md
	fileMounts := []fileMount{
		{filePath: "/workspace/task.md"},
	}

	pod := buildPod(task, "test-task-pod", task.Namespace, cfg, nil, fileMounts, nil, nil, defaultSystemConfig(), "")

	// Verify OPENCODE_CONFIG_CONTENT env var is NOT set
	container := pod.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == OpenCodeConfigContentEnvVar {
			t.Errorf("OPENCODE_CONFIG_CONTENT env var should not be set when no context file")
		}
	}
}
