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

func TestBuildJob_WithHumanInTheLoop_Sidecar(t *testing.T) {
	// Test that humanInTheLoop creates a session sidecar container instead of wrapping command
	duration := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo hello"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:  true,
			Duration: &duration,
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	// Verify there are 2 containers: agent and session sidecar
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2 (agent + sidecar)", len(containers))
	}

	// Verify agent container command is NOT wrapped (uses original command)
	agentContainer := containers[0]
	if agentContainer.Name != "agent" {
		t.Errorf("Containers[0].Name = %q, want %q", agentContainer.Name, "agent")
	}
	if len(agentContainer.Command) != 3 {
		t.Fatalf("len(agent Command) = %d, want 3", len(agentContainer.Command))
	}
	if agentContainer.Command[2] != "echo hello" {
		t.Errorf("Agent command should be original 'echo hello', got: %s", agentContainer.Command[2])
	}

	// Verify sidecar container
	sidecar := containers[1]
	if sidecar.Name != "session" {
		t.Errorf("Containers[1].Name = %q, want %q", sidecar.Name, "session")
	}
	if sidecar.Image != "test-agent:v1.0.0" {
		t.Errorf("Sidecar image = %q, want %q (same as agent)", sidecar.Image, "test-agent:v1.0.0")
	}
	// Verify sidecar has the same ImagePullPolicy as agent
	if sidecar.ImagePullPolicy != agentContainer.ImagePullPolicy {
		t.Errorf("Sidecar ImagePullPolicy = %q, agent ImagePullPolicy = %q, should be equal",
			sidecar.ImagePullPolicy, agentContainer.ImagePullPolicy)
	}
	// Verify sidecar command is sleep with correct duration
	if len(sidecar.Command) != 2 {
		t.Fatalf("len(sidecar Command) = %d, want 2", len(sidecar.Command))
	}
	if sidecar.Command[0] != "sleep" {
		t.Errorf("Sidecar Command[0] = %q, want %q", sidecar.Command[0], "sleep")
	}
	if sidecar.Command[1] != "1800" {
		t.Errorf("Sidecar Command[1] = %q, want %q (30 minutes)", sidecar.Command[1], "1800")
	}

	// Verify sidecar shares the same working directory
	if sidecar.WorkingDir != "/workspace" {
		t.Errorf("Sidecar WorkingDir = %q, want %q", sidecar.WorkingDir, "/workspace")
	}

	// Verify sidecar has the same volume mounts as agent
	if len(sidecar.VolumeMounts) != len(agentContainer.VolumeMounts) {
		t.Errorf("Sidecar should have same number of volume mounts as agent")
	}

	// Verify session duration env var in agent container
	var foundDurationEnv bool
	for _, env := range agentContainer.Env {
		if env.Name == EnvHumanInTheLoopDuration {
			foundDurationEnv = true
			if env.Value != "1800" {
				t.Errorf("KUBETASK_SESSION_DURATION_SECONDS = %q, want %q", env.Value, "1800")
			}
		}
	}
	if !foundDurationEnv {
		t.Errorf("KUBETASK_SESSION_DURATION_SECONDS env not found in agent container")
	}
}

func TestBuildJob_WithHumanInTheLoop_CustomImage(t *testing.T) {
	// Test that custom image can be specified for the sidecar
	duration := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"python", "-c", "print('hello; world')"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:  true,
			Duration: &duration,
			Image:    "busybox:stable", // Custom lightweight image
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	// Verify there are 2 containers
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	// Verify agent container uses original command unmodified
	agentContainer := containers[0]
	if len(agentContainer.Command) != 3 {
		t.Fatalf("len(agent Command) = %d, want 3, got: %v", len(agentContainer.Command), agentContainer.Command)
	}
	if agentContainer.Command[0] != "python" {
		t.Errorf("Agent Command[0] = %q, want %q", agentContainer.Command[0], "python")
	}

	// Verify sidecar uses custom image
	sidecar := containers[1]
	if sidecar.Image != "busybox:stable" {
		t.Errorf("Sidecar image = %q, want %q", sidecar.Image, "busybox:stable")
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
	if initContainer.Name != "git-init-0" {
		t.Errorf("Init container name = %q, want %q", initContainer.Name, "git-init-0")
	}
	if initContainer.Image != DefaultGitInitImage {
		t.Errorf("Init container image = %q, want %q", initContainer.Image, DefaultGitInitImage)
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

	container := buildGitInitContainer(gm, "git-vol-0", 0)

	if container.Name != "git-init-0" {
		t.Errorf("Container name = %q, want %q", container.Name, "git-init-0")
	}

	if container.Image != DefaultGitInitImage {
		t.Errorf("Container image = %q, want %q", container.Image, DefaultGitInitImage)
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

func TestBuildJob_WithHumanInTheLoop_Ports(t *testing.T) {
	// Test that ports are applied to the sidecar container, not the agent
	duration := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "npm run dev"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:  true,
			Duration: &duration,
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
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	// Verify agent container has no ports
	agentContainer := containers[0]
	if len(agentContainer.Ports) != 0 {
		t.Errorf("Agent container should have no ports, got %d", len(agentContainer.Ports))
	}

	// Verify sidecar container has the ports
	sidecar := containers[1]
	if len(sidecar.Ports) != 2 {
		t.Fatalf("len(sidecar.Ports) = %d, want 2", len(sidecar.Ports))
	}

	// Verify first port
	if sidecar.Ports[0].Name != "dev-server" {
		t.Errorf("Ports[0].Name = %q, want %q", sidecar.Ports[0].Name, "dev-server")
	}
	if sidecar.Ports[0].ContainerPort != 3000 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d", sidecar.Ports[0].ContainerPort, 3000)
	}
	if sidecar.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("Ports[0].Protocol = %q, want %q", sidecar.Ports[0].Protocol, corev1.ProtocolTCP)
	}

	// Verify second port (with default protocol)
	if sidecar.Ports[1].Name != "api" {
		t.Errorf("Ports[1].Name = %q, want %q", sidecar.Ports[1].Name, "api")
	}
	if sidecar.Ports[1].ContainerPort != 8080 {
		t.Errorf("Ports[1].ContainerPort = %d, want %d", sidecar.Ports[1].ContainerPort, 8080)
	}
	if sidecar.Ports[1].Protocol != corev1.ProtocolTCP {
		t.Errorf("Ports[1].Protocol = %q, want %q (default)", sidecar.Ports[1].Protocol, corev1.ProtocolTCP)
	}
}

func TestBuildJob_WithHumanInTheLoop_Disabled(t *testing.T) {
	// Test that when humanInTheLoop is disabled, no sidecar is created
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled: false,
			Ports: []kubetaskv1alpha1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 80,
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	containers := job.Spec.Template.Spec.Containers

	// Only agent container should exist, no sidecar
	if len(containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1 (no sidecar when disabled)", len(containers))
	}

	// Agent container should have no ports (ports only apply to sidecar)
	if len(containers[0].Ports) != 0 {
		t.Errorf("Agent container should have no ports when humanInTheLoop is disabled, got %d", len(containers[0].Ports))
	}
}

func TestBuildJob_WithHumanInTheLoop_UDPPort(t *testing.T) {
	// Test that UDP protocol is respected on sidecar ports
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled: true,
			Ports: []kubetaskv1alpha1.ContainerPort{
				{
					Name:          "dns",
					ContainerPort: 53,
					Protocol:      corev1.ProtocolUDP,
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	// Verify UDP protocol is respected on sidecar
	sidecar := containers[1]
	if len(sidecar.Ports) != 1 {
		t.Fatalf("len(sidecar.Ports) = %d, want 1", len(sidecar.Ports))
	}
	if sidecar.Ports[0].Protocol != corev1.ProtocolUDP {
		t.Errorf("Ports[0].Protocol = %q, want %q", sidecar.Ports[0].Protocol, corev1.ProtocolUDP)
	}
}

func TestBuildJob_WithHumanInTheLoop_FromAgent(t *testing.T) {
	// Test that humanInTheLoop from Agent creates sidecar with correct configuration
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	duration := metav1.Duration{Duration: 2 * time.Hour}
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:  true,
			Duration: &duration,
			Ports: []kubetaskv1alpha1.ContainerPort{
				{
					Name:          "agent-port",
					ContainerPort: 9000,
				},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	// Verify sidecar has correct sleep duration (2 hours = 7200 seconds)
	sidecar := containers[1]
	if sidecar.Command[1] != "7200" {
		t.Errorf("Sidecar Command[1] = %q, want %q (2 hours)", sidecar.Command[1], "7200")
	}

	// Verify ports from Agent's humanInTheLoop are applied to sidecar
	if len(sidecar.Ports) != 1 {
		t.Fatalf("len(sidecar.Ports) = %d, want 1", len(sidecar.Ports))
	}
	if sidecar.Ports[0].Name != "agent-port" {
		t.Errorf("Ports[0].Name = %q, want %q", sidecar.Ports[0].Name, "agent-port")
	}
	if sidecar.Ports[0].ContainerPort != 9000 {
		t.Errorf("Ports[0].ContainerPort = %d, want %d", sidecar.Ports[0].ContainerPort, 9000)
	}
}

func TestBuildJob_WithHumanInTheLoop_DefaultDuration(t *testing.T) {
	// Test that default duration (1 hour) is used when not specified
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo test"},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled: true,
			// Duration not specified, should use default (1 hour)
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	// Verify sidecar uses default duration (1 hour = 3600 seconds)
	sidecar := containers[1]
	if sidecar.Command[1] != "3600" {
		t.Errorf("Sidecar Command[1] = %q, want %q (default 1 hour)", sidecar.Command[1], "3600")
	}
}

func TestBuildJob_WithHumanInTheLoop_SidecarSharesAllMounts(t *testing.T) {
	// Test that sidecar container has all the same mounts as agent container
	// including context ConfigMap, directory mounts, and git mounts
	duration := metav1.Duration{Duration: 30 * time.Minute}
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	envVarName := "GITHUB_TOKEN"
	secretKey := "token"
	mountPath := "/home/agent/.ssh/id_rsa"
	cfg := agentConfig{
		agentImage:         "test-agent:v1.0.0",
		workspaceDir:       "/workspace",
		serviceAccountName: "test-sa",
		command:            []string{"sh", "-c", "echo hello"},
		credentials: []kubetaskv1alpha1.Credential{
			{
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "github-creds",
					Key:  &secretKey,
				},
				Env: &envVarName,
			},
			{
				SecretRef: kubetaskv1alpha1.SecretReference{
					Name: "ssh-key",
					Key:  &secretKey,
				},
				MountPath: &mountPath,
			},
		},
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled:  true,
			Duration: &duration,
		},
	}

	// Create context ConfigMap with file mounts
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

	dirMounts := []dirMount{
		{dirPath: "/workspace/config", configMapName: "my-config", optional: false},
	}

	gitMounts := []gitMount{
		{
			contextName: "my-repo",
			repository:  "https://github.com/example/repo",
			ref:         "main",
			mountPath:   "/workspace/src",
		},
	}

	job := buildJob(task, "test-task-job", cfg, contextConfigMap, fileMounts, dirMounts, gitMounts)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(containers))
	}

	agentContainer := containers[0]
	sidecar := containers[1]

	// Verify sidecar has the same number of volume mounts as agent
	if len(sidecar.VolumeMounts) != len(agentContainer.VolumeMounts) {
		t.Errorf("Sidecar VolumeMounts count = %d, agent VolumeMounts count = %d, should be equal",
			len(sidecar.VolumeMounts), len(agentContainer.VolumeMounts))
	}

	// Verify specific mounts exist on both agent and sidecar
	agentMountPaths := make(map[string]bool)
	for _, vm := range agentContainer.VolumeMounts {
		agentMountPaths[vm.MountPath] = true
	}

	sidecarMountPaths := make(map[string]bool)
	for _, vm := range sidecar.VolumeMounts {
		sidecarMountPaths[vm.MountPath] = true
	}

	// Verify all expected mounts are present
	expectedMounts := []string{
		"/workspace/task.md",      // Context ConfigMap file mount
		"/workspace/config",       // Directory mount
		"/workspace/src",          // Git mount
		"/home/agent/.ssh/id_rsa", // Credential file mount
	}

	for _, path := range expectedMounts {
		if !agentMountPaths[path] {
			t.Errorf("Agent container missing expected mount: %s", path)
		}
		if !sidecarMountPaths[path] {
			t.Errorf("Sidecar container missing expected mount: %s (should have same mounts as agent)", path)
		}
	}

	// Verify sidecar has the same environment variables as agent
	if len(sidecar.Env) != len(agentContainer.Env) {
		t.Errorf("Sidecar Env count = %d, agent Env count = %d, should be equal",
			len(sidecar.Env), len(agentContainer.Env))
	}

	// Verify sidecar has the same EnvFrom as agent
	if len(sidecar.EnvFrom) != len(agentContainer.EnvFrom) {
		t.Errorf("Sidecar EnvFrom count = %d, agent EnvFrom count = %d, should be equal",
			len(sidecar.EnvFrom), len(agentContainer.EnvFrom))
	}
}

func TestBuildJob_WithHumanInTheLoop_CustomCommand(t *testing.T) {
	// Test that humanInTheLoop with custom Command uses the command instead of sleep
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: kubetaskv1alpha1.TaskSpec{},
	}
	task.APIVersion = "kubetask.io/v1alpha1"
	task.Kind = "Task"

	customCommand := []string{"sh", "-c", "code-server --bind-addr 0.0.0.0:8080 & sleep 7200"}
	cfg := agentConfig{
		agentImage:   "test-agent:v1",
		command:      []string{"gemini", "--yolo", "-p", "$(cat /workspace/task.md)"},
		workspaceDir: "/workspace",
		humanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
			Enabled: true,
			Command: customCommand,
			Image:   "code-server:latest",
			Ports: []kubetaskv1alpha1.ContainerPort{
				{Name: "code-server", ContainerPort: 8080},
			},
		},
	}

	job := buildJob(task, "test-task-job", cfg, nil, nil, nil, nil)

	// Verify we have 2 containers (agent + sidecar)
	if len(job.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("Expected 2 containers, got %d", len(job.Spec.Template.Spec.Containers))
	}

	agentContainer := job.Spec.Template.Spec.Containers[0]
	sidecar := job.Spec.Template.Spec.Containers[1]

	// Verify agent container has original command (not modified)
	if len(agentContainer.Command) != 4 || agentContainer.Command[0] != "gemini" {
		t.Errorf("Agent command was unexpectedly modified: %v", agentContainer.Command)
	}

	// Verify sidecar uses custom command, not sleep
	if len(sidecar.Command) != 3 {
		t.Fatalf("Sidecar command length = %d, want 3", len(sidecar.Command))
	}
	if sidecar.Command[0] != "sh" || sidecar.Command[1] != "-c" {
		t.Errorf("Sidecar command = %v, expected sh -c ...", sidecar.Command)
	}
	if sidecar.Command[2] != "code-server --bind-addr 0.0.0.0:8080 & sleep 7200" {
		t.Errorf("Sidecar command = %v, expected custom command", sidecar.Command)
	}

	// Verify sidecar uses custom image
	if sidecar.Image != "code-server:latest" {
		t.Errorf("Sidecar Image = %q, want %q", sidecar.Image, "code-server:latest")
	}

	// Verify sidecar has the port exposed
	if len(sidecar.Ports) != 1 {
		t.Fatalf("Sidecar ports count = %d, want 1", len(sidecar.Ports))
	}
	if sidecar.Ports[0].ContainerPort != 8080 {
		t.Errorf("Sidecar port = %d, want 8080", sidecar.Ports[0].ContainerPort)
	}
}
