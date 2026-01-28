// Copyright Contributors to the KubeOpenCode project

package controller

import (
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

const (
	// ServerDeploymentSuffix is appended to Agent name for the Deployment name.
	ServerDeploymentSuffix = "-server"

	// ServerContainerName is the name of the main container in the server Deployment.
	ServerContainerName = "opencode-server"

	// DefaultServerPort is the default port for OpenCode server.
	DefaultServerPort int32 = 4096

	// ServerHealthPath is the path used for readiness probes.
	// OpenCode's /session/status endpoint returns 200 if the server is healthy.
	ServerHealthPath = "/session/status"
)

// ServerDeploymentName returns the Deployment name for a Server-mode Agent.
func ServerDeploymentName(agentName string) string {
	return agentName + ServerDeploymentSuffix
}

// ServerServiceName returns the Service name for a Server-mode Agent.
// We use the Agent name directly for simpler DNS resolution.
func ServerServiceName(agentName string) string {
	return agentName
}

// ServerURL returns the in-cluster URL for a Server-mode Agent.
func ServerURL(agentName, namespace string, port int32) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", agentName, namespace, port)
}

// BuildServerDeployment creates a Deployment for a Server-mode Agent.
// The Deployment runs OpenCode in serve mode with a single replica.
func BuildServerDeployment(agent *kubeopenv1alpha1.Agent, agentCfg agentConfig, sysCfg systemConfig) *appsv1.Deployment {
	serverConfig := agent.Spec.ServerConfig
	if serverConfig == nil {
		return nil
	}

	port := serverConfig.Port
	if port == 0 {
		port = DefaultServerPort
	}

	// Build labels for selector and pod template
	labels := map[string]string{
		"app.kubernetes.io/name":       "kubeopencode-server",
		"app.kubernetes.io/instance":   agent.Name,
		"app.kubernetes.io/component":  "server",
		"app.kubernetes.io/managed-by": "kubeopencode",
		"kubeopencode.io/agent":        agent.Name,
	}

	// Merge custom labels from PodSpec if provided
	if agentCfg.podSpec != nil && agentCfg.podSpec.Labels != nil {
		maps.Copy(labels, agentCfg.podSpec.Labels)
	}

	// Build environment variables
	envVars := []corev1.EnvVar{
		{Name: EnvWorkspaceDir, Value: agentCfg.workspaceDir},
		// OpenCode permission settings for automated execution
		{Name: OpenCodePermissionEnvVar, Value: DefaultOpenCodePermission},
	}

	// Add OpenCode config if provided
	if agentCfg.config != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  OpenCodeConfigEnvVar,
			Value: OpenCodeConfigPath,
		})
	}

	// Build volume mounts
	volumeMounts := []corev1.VolumeMount{
		{Name: ToolsVolumeName, MountPath: ToolsMountPath},
		{Name: WorkspaceVolumeName, MountPath: agentCfg.workspaceDir},
	}

	// Build volumes
	volumes := []corev1.Volume{
		{
			Name: ToolsVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: WorkspaceVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Build command for OpenCode serve mode
	command := []string{
		"sh", "-c",
		fmt.Sprintf("/tools/opencode serve --port %d --hostname 0.0.0.0", port),
	}

	// Build the main container
	container := corev1.Container{
		Name:            ServerContainerName,
		Image:           agentCfg.executorImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         command,
		Env:             envVars,
		VolumeMounts:    volumeMounts,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		// Liveness probe: TCP check on the server port
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(port),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		// Readiness probe: HTTP check on /session/status
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   ServerHealthPath,
					Port:   intstr.FromInt32(port),
					Scheme: corev1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
	}

	// Apply resource requirements if specified in podSpec
	if agentCfg.podSpec != nil && agentCfg.podSpec.Resources != nil {
		container.Resources = *agentCfg.podSpec.Resources
	}

	// Build init container to copy OpenCode binary
	initContainer := buildOpenCodeInitContainer(agentCfg.agentImage)

	// Build pod template spec
	podSpec := corev1.PodSpec{
		ServiceAccountName: agentCfg.serviceAccountName,
		InitContainers:     []corev1.Container{initContainer},
		Containers:         []corev1.Container{container},
		Volumes:            volumes,
		RestartPolicy:      corev1.RestartPolicyAlways,
	}

	// Apply scheduling configuration if provided
	if agentCfg.podSpec != nil && agentCfg.podSpec.Scheduling != nil {
		scheduling := agentCfg.podSpec.Scheduling
		if scheduling.NodeSelector != nil {
			podSpec.NodeSelector = scheduling.NodeSelector
		}
		if scheduling.Tolerations != nil {
			podSpec.Tolerations = scheduling.Tolerations
		}
		if scheduling.Affinity != nil {
			podSpec.Affinity = scheduling.Affinity
		}
	}

	// Apply runtime class if specified
	if agentCfg.podSpec != nil && agentCfg.podSpec.RuntimeClassName != nil {
		podSpec.RuntimeClassName = agentCfg.podSpec.RuntimeClassName
	}

	// Single replica for now (simplicity)
	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServerDeploymentName(agent.Name),
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubeopencode.io/agent": agent.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildServerService creates a Service for a Server-mode Agent.
func BuildServerService(agent *kubeopenv1alpha1.Agent) *corev1.Service {
	serverConfig := agent.Spec.ServerConfig
	if serverConfig == nil {
		return nil
	}

	port := serverConfig.Port
	if port == 0 {
		port = DefaultServerPort
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "kubeopencode-server",
		"app.kubernetes.io/instance":   agent.Name,
		"app.kubernetes.io/component":  "server",
		"app.kubernetes.io/managed-by": "kubeopencode",
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServerServiceName(agent.Name),
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"kubeopencode.io/agent": agent.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// IsServerMode returns true if the Agent is configured for Server mode.
func IsServerMode(agent *kubeopenv1alpha1.Agent) bool {
	return agent.Spec.ServerConfig != nil
}

// GetServerPort returns the configured port or default.
func GetServerPort(agent *kubeopenv1alpha1.Agent) int32 {
	if agent.Spec.ServerConfig != nil && agent.Spec.ServerConfig.Port != 0 {
		return agent.Spec.ServerConfig.Port
	}
	return DefaultServerPort
}
