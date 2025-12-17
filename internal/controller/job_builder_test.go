// Copyright Contributors to the KubeTask project

//go:build !integration

package controller

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

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

func TestBuildJob_BasicTask(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	// Verify job metadata
	if job.Name != "test-task-job" {
		t.Errorf("Job.Name = %q, want %q", job.Name, "test-task-job")
	}
	if job.Namespace != "default" {
		t.Errorf("Job.Namespace = %q, want %q", job.Namespace, "default")
	}

	// Verify labels
	if job.Labels["app"] != "kubetask" {
		t.Errorf("Job.Labels[app] = %q, want %q", job.Labels["app"], "kubetask")
	}
	if job.Labels["kubetask.io/task"] != "test-task" {
		t.Errorf("Job.Labels[kubetask.io/task] = %q, want %q", job.Labels["kubetask.io/task"], "test-task")
	}

	// Verify owner reference
	if len(job.OwnerReferences) != 1 {
		t.Fatalf("len(Job.OwnerReferences) = %d, want 1", len(job.OwnerReferences))
	}
	ownerRef := job.OwnerReferences[0]
	if ownerRef.Name != "test-task" {
		t.Errorf("OwnerReference.Name = %q, want %q", ownerRef.Name, "test-task")
	}
	if ownerRef.Controller == nil || *ownerRef.Controller != true {
		t.Errorf("OwnerReference.Controller = %v, want true", ownerRef.Controller)
	}

	// Verify container
	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1", len(job.Spec.Template.Spec.Containers))
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "agent" {
		t.Errorf("Container.Name = %q, want %q", container.Name, "agent")
	}
	if container.Image != "test-agent:v1.0.0" {
		t.Errorf("Container.Image = %q, want %q", container.Image, "test-agent:v1.0.0")
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
	if job.Spec.Template.Spec.ServiceAccountName != "test-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", job.Spec.Template.Spec.ServiceAccountName, "test-sa")
	}

	// Verify restart policy
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want %q", job.Spec.Template.Spec.RestartPolicy, corev1.RestartPolicyNever)
	}

	// Verify backoff limit is 0 (no retries - AI tasks are not idempotent)
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("BackoffLimit = %v, want 0", job.Spec.BackoffLimit)
	}
}

// stringPtr returns a pointer to the given string value
func stringPtr(s string) *string {
	return &s
}

func TestBuildJob_WithCredentials(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	envName := "API_TOKEN"
	mountPath := "/home/agent/.ssh/id_rsa"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		credentials: []kubetaskv1alpha1.Credential{
			{
				Name: "api-token",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "my-secret",
					Key:  stringPtr("token"),
				},
				Env: &envName,
			},
			{
				Name: "ssh-key",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "ssh-secret",
					Key:  stringPtr("private-key"),
				},
				MountPath: &mountPath,
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

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
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.Secret != nil && vol.Secret.SecretName == "ssh-secret" {
			foundVolume = true
		}
	}
	if !foundVolume {
		t.Errorf("Secret volume for ssh-secret not found")
	}
}

func TestBuildJob_WithEntireSecretCredential(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		credentials: []kubetaskv1alpha1.Credential{
			{
				// No Key specified - mount entire secret as env vars
				Name: "api-keys",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "api-credentials",
					// Key is nil - entire secret should be mounted
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

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

func TestBuildJob_WithMixedCredentials(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	envName := "GITHUB_TOKEN"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		credentials: []kubetaskv1alpha1.Credential{
			{
				// Entire secret mount (no key)
				Name: "all-api-keys",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "api-credentials",
				},
			},
			{
				// Single key mount with env rename
				Name: "github-token",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "github-secret",
					Key:  stringPtr("token"),
				},
				Env: &envName,
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

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

func TestBuildJob_WithEntireSecretAsDirectory(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	mountPath := "/etc/ssl/certs"
	var fileMode int32 = 0400

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		credentials: []kubetaskv1alpha1.Credential{
			{
				// No Key specified + MountPath = mount entire secret as directory
				Name: "tls-certs",
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "tls-certificates",
					// Key is nil - entire secret should be mounted as directory
				},
				MountPath: &mountPath,
				FileMode:  &fileMode,
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]
	podSpec := job.Spec.Template.Spec

	// Verify envFrom is NOT set (should not be env vars)
	if len(container.EnvFrom) != 0 {
		t.Errorf("Expected 0 envFrom entries, got %d", len(container.EnvFrom))
	}

	// Verify volume is created
	var foundVolume bool
	var volumeName string
	for _, vol := range podSpec.Volumes {
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

func TestBuildJob_WithHumanInTheLoop(t *testing.T) {
	keepAlive := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled:   true,
				KeepAlive: &keepAlive,
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo hello"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify command is wrapped
	if len(container.Command) != 3 {
		t.Fatalf("len(Command) = %d, want 3", len(container.Command))
	}
	if container.Command[0] != "sh" {
		t.Errorf("Command[0] = %q, want %q", container.Command[0], "sh")
	}
	if container.Command[1] != "-c" {
		t.Errorf("Command[1] = %q, want %q", container.Command[1], "-c")
	}

	// Verify wrapped script contains sleep
	script := container.Command[2]
	if !contains(script, "sleep 1800") {
		t.Errorf("Command script should contain 'sleep 1800', got: %s", script)
	}
	if !contains(script, "Human-in-the-loop") {
		t.Errorf("Command script should contain 'Human-in-the-loop', got: %s", script)
	}
	// Since the original command is already "sh -c <script>", we extract the inner script
	// and wrap it in a subshell ( ) to isolate exit/exec commands
	if !contains(script, "( echo hello )") {
		t.Errorf("Command script should contain inner script in subshell '( echo hello )', got: %s", script)
	}

	// Verify keep-alive env var
	var foundKeepAliveEnv bool
	for _, env := range container.Env {
		if env.Name == EnvHumanInTheLoopKeepAlive {
			foundKeepAliveEnv = true
			if env.Value != "1800" {
				t.Errorf("KUBETASK_KEEP_ALIVE_SECONDS = %q, want %q", env.Value, "1800")
			}
		}
	}
	if !foundKeepAliveEnv {
		t.Errorf("KUBETASK_KEEP_ALIVE_SECONDS env not found")
	}
}

func TestBuildJob_WithHumanInTheLoop_NonShCCommand(t *testing.T) {
	// Test that non "sh -c" commands are handled correctly using $@ approach
	keepAlive := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled:   true,
				KeepAlive: &keepAlive,
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	// Non sh-c command with arguments that contain special characters
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"python", "-c", "print('hello; world')"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify command uses $@ approach for non sh-c commands
	// Format: ["sh", "-c", '"$@"; EXIT_CODE=$?; ...', "--", "python", "-c", "print('hello; world')"]
	if len(container.Command) != 7 {
		t.Fatalf("len(Command) = %d, want 7, got: %v", len(container.Command), container.Command)
	}
	if container.Command[0] != "sh" {
		t.Errorf("Command[0] = %q, want %q", container.Command[0], "sh")
	}
	if container.Command[1] != "-c" {
		t.Errorf("Command[1] = %q, want %q", container.Command[1], "-c")
	}
	// Command[2] is the wrapper script
	script := container.Command[2]
	if !contains(script, `"$@"`) {
		t.Errorf("Command script should use $@ for argument passing, got: %s", script)
	}
	if !contains(script, "sleep 1800") {
		t.Errorf("Command script should contain 'sleep 1800', got: %s", script)
	}
	// Command[3] should be "--" separator
	if container.Command[3] != "--" {
		t.Errorf("Command[3] = %q, want %q", container.Command[3], "--")
	}
	// Command[4:] should be the original command
	if container.Command[4] != "python" {
		t.Errorf("Command[4] = %q, want %q", container.Command[4], "python")
	}
	if container.Command[5] != "-c" {
		t.Errorf("Command[5] = %q, want %q", container.Command[5], "-c")
	}
	if container.Command[6] != "print('hello; world')" {
		t.Errorf("Command[6] = %q, want %q", container.Command[6], "print('hello; world')")
	}
}

func TestBuildJob_WithPodScheduling(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	runtimeClass := "gvisor"
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		podSpec: &kubetaskv1alpha1.AgentPodSpec{
			Labels: map[string]string{
				"custom-label": "custom-value",
			},
			Scheduling: &kubetaskv1alpha1.PodScheduling{
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

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	podSpec := job.Spec.Template.Spec

	// Verify node selector
	if podSpec.NodeSelector["node-type"] != "gpu" {
		t.Errorf("NodeSelector[node-type] = %q, want %q", podSpec.NodeSelector["node-type"], "gpu")
	}

	// Verify tolerations
	if len(podSpec.Tolerations) != 1 {
		t.Fatalf("len(Tolerations) = %d, want 1", len(podSpec.Tolerations))
	}
	if podSpec.Tolerations[0].Key != "dedicated" {
		t.Errorf("Tolerations[0].Key = %q, want %q", podSpec.Tolerations[0].Key, "dedicated")
	}

	// Verify runtime class
	if podSpec.RuntimeClassName == nil || *podSpec.RuntimeClassName != "gvisor" {
		t.Errorf("RuntimeClassName = %v, want %q", podSpec.RuntimeClassName, "gvisor")
	}

	// Verify custom label on pod template
	podLabels := job.Spec.Template.Labels
	if podLabels["custom-label"] != "custom-value" {
		t.Errorf("PodLabels[custom-label] = %q, want %q", podLabels["custom-label"], "custom-value")
	}
	// Verify base labels are still present
	if podLabels["app"] != "kubetask" {
		t.Errorf("PodLabels[app] = %q, want %q", podLabels["app"], "kubetask")
	}
}

func TestBuildJob_WithContextConfigMap(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
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

	job := buildJob(task, "test-task-job", cfg, contextConfigMap, fileMounts, nil, nil)

	// Verify context-files volume exists
	var foundContextVolume bool
	for _, vol := range job.Spec.Template.Spec.Volumes {
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

	// Verify volume mount exists
	container := job.Spec.Template.Spec.Containers[0]
	var foundMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace/task.md" {
			foundMount = true
			if mount.SubPath != "workspace-task.md" {
				t.Errorf("VolumeMount.SubPath = %q, want %q", mount.SubPath, "workspace-task.md")
			}
		}
	}
	if !foundMount {
		t.Errorf("Volume mount for /workspace/task.md not found")
	}
}

func TestBuildJob_WithDirMounts(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
	}

	dirMounts := []dirMount{
		{
			dirPath:       "/workspace/guides",
			configMapName: "guides-configmap",
			optional:      true,
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, dirMounts, nil)

	// Verify dir-mount volume exists
	var foundDirVolume bool
	for _, vol := range job.Spec.Template.Spec.Volumes {
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

	// Verify volume mount exists
	container := job.Spec.Template.Spec.Containers[0]
	var foundMount bool
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/workspace/guides" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Errorf("Volume mount for /workspace/guides not found")
	}
}

func TestBuildJob_WithGitMounts(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubetask.io/v1alpha1",
			Kind:       "Task",
		},
	}

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
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

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, gitMounts)

	// Verify init container exists
	if len(job.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("Expected 1 init container, got %d", len(job.Spec.Template.Spec.InitContainers))
	}

	initContainer := job.Spec.Template.Spec.InitContainers[0]
	if initContainer.Name != "git-sync-0" {
		t.Errorf("Init container name = %q, want %q", initContainer.Name, "git-sync-0")
	}
	if initContainer.Image != DefaultGitSyncImage {
		t.Errorf("Init container image = %q, want %q", initContainer.Image, DefaultGitSyncImage)
	}

	// Verify environment variables
	envMap := make(map[string]string)
	for _, env := range initContainer.Env {
		envMap[env.Name] = env.Value
	}
	if envMap["GITSYNC_REPO"] != "https://github.com/org/repo.git" {
		t.Errorf("GITSYNC_REPO = %q, want %q", envMap["GITSYNC_REPO"], "https://github.com/org/repo.git")
	}
	if envMap["GITSYNC_REF"] != "main" {
		t.Errorf("GITSYNC_REF = %q, want %q", envMap["GITSYNC_REF"], "main")
	}
	if envMap["GITSYNC_ONE_TIME"] != "true" {
		t.Errorf("GITSYNC_ONE_TIME = %q, want %q", envMap["GITSYNC_ONE_TIME"], "true")
	}
	if envMap["GITSYNC_DEPTH"] != "1" {
		t.Errorf("GITSYNC_DEPTH = %q, want %q", envMap["GITSYNC_DEPTH"], "1")
	}

	// Verify emptyDir volume exists
	var foundGitVolume bool
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.Name == "git-context-0" && vol.EmptyDir != nil {
			foundGitVolume = true
		}
	}
	if !foundGitVolume {
		t.Errorf("git-context-0 emptyDir volume not found")
	}

	// Verify volume mount in agent container with correct subPath
	container := job.Spec.Template.Spec.Containers[0]
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

func TestBuildJob_WithGitMountsAndAuth(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubetask.io/v1alpha1",
			Kind:       "Task",
		},
	}

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
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

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, gitMounts)

	// Verify init container has auth env vars
	initContainer := job.Spec.Template.Spec.InitContainers[0]
	var foundUsername, foundPassword bool
	for _, env := range initContainer.Env {
		if env.Name == "GITSYNC_USERNAME" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			if env.ValueFrom.SecretKeyRef.Name == "git-credentials" && env.ValueFrom.SecretKeyRef.Key == "username" {
				foundUsername = true
			}
		}
		if env.Name == "GITSYNC_PASSWORD" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			if env.ValueFrom.SecretKeyRef.Name == "git-credentials" && env.ValueFrom.SecretKeyRef.Key == "password" {
				foundPassword = true
			}
		}
	}
	if !foundUsername {
		t.Errorf("GITSYNC_USERNAME env var with secret reference not found")
	}
	if !foundPassword {
		t.Errorf("GITSYNC_PASSWORD env var with secret reference not found")
	}

	// Verify volume mount without subPath (entire repo)
	container := job.Spec.Template.Spec.Containers[0]
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

func TestBuildGitSyncInitContainer(t *testing.T) {
	gm := gitMount{
		contextName: "test-context",
		repository:  "https://github.com/test/repo.git",
		ref:         "develop",
		repoPath:    "docs/",
		mountPath:   "/workspace/docs",
		depth:       5,
		secretName:  "",
	}

	container := buildGitSyncInitContainer(gm, "git-vol-0", 0)

	if container.Name != "git-sync-0" {
		t.Errorf("Container name = %q, want %q", container.Name, "git-sync-0")
	}

	if container.Image != DefaultGitSyncImage {
		t.Errorf("Container image = %q, want %q", container.Image, DefaultGitSyncImage)
	}

	// Check env vars
	envMap := make(map[string]string)
	for _, env := range container.Env {
		if env.Value != "" {
			envMap[env.Name] = env.Value
		}
	}

	if envMap["GITSYNC_REPO"] != "https://github.com/test/repo.git" {
		t.Errorf("GITSYNC_REPO = %q, want %q", envMap["GITSYNC_REPO"], "https://github.com/test/repo.git")
	}
	if envMap["GITSYNC_REF"] != "develop" {
		t.Errorf("GITSYNC_REF = %q, want %q", envMap["GITSYNC_REF"], "develop")
	}
	if envMap["GITSYNC_DEPTH"] != "5" {
		t.Errorf("GITSYNC_DEPTH = %q, want %q", envMap["GITSYNC_DEPTH"], "5")
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

func TestBuildJob_WithHumanInTheLoop_Ports(t *testing.T) {
	keepAlive := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled:   true,
				KeepAlive: &keepAlive,
				Ports: []kubetaskv1alpha1.ContainerPort{
					{
						Name:          "dev-server",
						ContainerPort: 3000,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "api",
						ContainerPort: 8080,
						// Protocol not specified, should default to TCP
					},
				},
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "npm run dev"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify container ports are set
	if len(container.Ports) != 2 {
		t.Fatalf("len(container.Ports) = %d, want 2", len(container.Ports))
	}

	// Verify first port
	if container.Ports[0].Name != "dev-server" {
		t.Errorf("Ports[0].Name = %q, want %q", container.Ports[0].Name, "dev-server")
	}
	if container.Ports[0].ContainerPort != 3000 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d", container.Ports[0].ContainerPort, 3000)
	}
	if container.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("Ports[0].Protocol = %q, want %q", container.Ports[0].Protocol, corev1.ProtocolTCP)
	}

	// Verify second port (with default protocol)
	if container.Ports[1].Name != "api" {
		t.Errorf("Ports[1].Name = %q, want %q", container.Ports[1].Name, "api")
	}
	if container.Ports[1].ContainerPort != 8080 {
		t.Errorf("Ports[1].ContainerPort = %d, want %d", container.Ports[1].ContainerPort, 8080)
	}
	if container.Ports[1].Protocol != corev1.ProtocolTCP {
		t.Errorf("Ports[1].Protocol = %q, want %q (default)", container.Ports[1].Protocol, corev1.ProtocolTCP)
	}
}

func TestBuildJob_WithHumanInTheLoop_PortsDisabled(t *testing.T) {
	// Test that ports can be specified even when humanInTheLoop.enabled is false
	// (ports are still useful for the container, just not the keep-alive behavior)
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled: false,
				Ports: []kubetaskv1alpha1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 80,
					},
				},
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Ports should still be applied even when enabled is false
	if len(container.Ports) != 1 {
		t.Fatalf("len(container.Ports) = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].ContainerPort != 80 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d", container.Ports[0].ContainerPort, 80)
	}
}

func TestBuildJob_WithHumanInTheLoop_UDPPort(t *testing.T) {
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled: true,
				Ports: []kubetaskv1alpha1.ContainerPort{
					{
						Name:          "dns",
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify UDP protocol is respected
	if len(container.Ports) != 1 {
		t.Fatalf("len(container.Ports) = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].Protocol != corev1.ProtocolUDP {
		t.Errorf("Ports[0].Protocol = %q, want %q", container.Ports[0].Protocol, corev1.ProtocolUDP)
	}
}

func TestBuildJob_WithHumanInTheLoop_FromAgent(t *testing.T) {
	// Test that humanInTheLoop from Agent is used when Task doesn't specify it
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			// No humanInTheLoop specified in Task
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	keepAlive := metav1.Duration{Duration: 2 * time.Hour}
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:   true,
			KeepAlive: &keepAlive,
			Ports: []kubetaskv1alpha1.ContainerPort{
				{
					Name:          "agent-port",
					ContainerPort: 9000,
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify command is wrapped with sleep (from Agent's humanInTheLoop)
	script := container.Command[2]
	if !contains(script, "sleep 7200") {
		t.Errorf("Command script should contain 'sleep 7200' (2 hours from Agent), got: %s", script)
	}

	// Verify ports from Agent's humanInTheLoop are applied
	if len(container.Ports) != 1 {
		t.Fatalf("len(container.Ports) = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].Name != "agent-port" {
		t.Errorf("Ports[0].Name = %q, want %q", container.Ports[0].Name, "agent-port")
	}
	if container.Ports[0].ContainerPort != 9000 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d", container.Ports[0].ContainerPort, 9000)
	}
}

func TestBuildJob_WithHumanInTheLoop_TaskOverridesAgent(t *testing.T) {
	// Test that Task's humanInTheLoop overrides Agent's
	taskKeepAlive := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled:   true,
				KeepAlive: &taskKeepAlive,
				Ports: []kubetaskv1alpha1.ContainerPort{
					{
						Name:          "task-port",
						ContainerPort: 8000,
					},
				},
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	agentKeepAlive := metav1.Duration{Duration: 2 * time.Hour}
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:   true,
			KeepAlive: &agentKeepAlive,
			Ports: []kubetaskv1alpha1.ContainerPort{
				{
					Name:          "agent-port",
					ContainerPort: 9000,
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify command uses Task's keepAlive (30 minutes = 1800 seconds), not Agent's
	script := container.Command[2]
	if !contains(script, "sleep 1800") {
		t.Errorf("Command script should contain 'sleep 1800' (30m from Task), got: %s", script)
	}
	if contains(script, "sleep 7200") {
		t.Errorf("Command script should NOT contain 'sleep 7200' (Agent's value), got: %s", script)
	}

	// Verify ports from Task's humanInTheLoop are used, not Agent's
	if len(container.Ports) != 1 {
		t.Fatalf("len(container.Ports) = %d, want 1", len(container.Ports))
	}
	if container.Ports[0].Name != "task-port" {
		t.Errorf("Ports[0].Name = %q, want %q (from Task)", container.Ports[0].Name, "task-port")
	}
	if container.Ports[0].ContainerPort != 8000 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d (from Task)", container.Ports[0].ContainerPort, 8000)
	}
}

func TestBuildJob_WithHumanInTheLoop_TaskDisablesAgentDefault(t *testing.T) {
	// Test that Task can disable humanInTheLoop even when Agent has it enabled
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
				Enabled: false, // Task explicitly disables
			},
		},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	agentKeepAlive := metav1.Duration{Duration: 2 * time.Hour}
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:   true, // Agent has it enabled
			KeepAlive: &agentKeepAlive,
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	container := job.Spec.Template.Spec.Containers[0]

	// Verify command is NOT wrapped (Task disabled humanInTheLoop)
	if len(container.Command) != 3 {
		t.Fatalf("len(Command) = %d, want 3 (unwrapped command)", len(container.Command))
	}
	script := container.Command[2]
	if contains(script, "sleep") {
		t.Errorf("Command should NOT contain sleep (Task disabled humanInTheLoop), got: %s", script)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
