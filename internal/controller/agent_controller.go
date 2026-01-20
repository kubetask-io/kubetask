// Copyright Contributors to the KubeOpenCode project

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

const (
	// AgentConditionServerReady indicates whether the OpenCode server is ready.
	AgentConditionServerReady = "ServerReady"

	// AgentConditionServerHealthy indicates whether the server is responding to health checks.
	// In the Pod-based approach, this is based on Deployment readiness rather than HTTP health checks.
	AgentConditionServerHealthy = "ServerHealthy"

	// DefaultServerReconcileInterval is how often to reconcile Server-mode Agents.
	DefaultServerReconcileInterval = 30 * time.Second
)

// AgentReconciler reconciles Agent resources.
// For Server-mode Agents, it manages the Deployment and Service.
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubeopencode.io,resources=agents,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kubeopencode.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Agent reconciliation.
// For Server-mode Agents, it ensures the Deployment and Service exist and are up-to-date.
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Agent
	var agent kubeopenv1alpha1.Agent
	if err := r.Get(ctx, req.NamespacedName, &agent); err != nil {
		if apierrors.IsNotFound(err) {
			// Agent was deleted, nothing to do (Deployment/Service will be garbage collected)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Agent")
		return ctrl.Result{}, err
	}

	// Only handle Server-mode Agents
	if !IsServerMode(&agent) {
		// Not a Server-mode Agent, clean up any stale server resources
		if err := r.cleanupServerResources(ctx, &agent); err != nil {
			logger.Error(err, "Failed to cleanup server resources")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling Server-mode Agent", "agent", agent.Name)

	// Resolve agent configuration
	agentCfg := r.resolveAgentConfig(&agent)
	sysCfg := systemConfig{
		systemImage:           DefaultKubeOpenCodeImage,
		systemImagePullPolicy: corev1.PullIfNotPresent,
	}

	// Reconcile the Deployment
	if err := r.reconcileDeployment(ctx, &agent, agentCfg, sysCfg); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile the Service
	if err := r.reconcileService(ctx, &agent); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Update Agent status
	if err := r.updateAgentStatus(ctx, &agent); err != nil {
		logger.Error(err, "Failed to update Agent status")
		return ctrl.Result{}, err
	}

	// Requeue periodically to check server health
	return ctrl.Result{RequeueAfter: DefaultServerReconcileInterval}, nil
}

// resolveAgentConfig extracts configuration from the Agent spec.
func (r *AgentReconciler) resolveAgentConfig(agent *kubeopenv1alpha1.Agent) agentConfig {
	cfg := agentConfig{
		agentImage:         agent.Spec.AgentImage,
		executorImage:      agent.Spec.ExecutorImage,
		attachImage:        agent.Spec.AttachImage,
		command:            agent.Spec.Command,
		workspaceDir:       agent.Spec.WorkspaceDir,
		contexts:           agent.Spec.Contexts,
		config:             agent.Spec.Config,
		credentials:        agent.Spec.Credentials,
		podSpec:            agent.Spec.PodSpec,
		serviceAccountName: agent.Spec.ServiceAccountName,
		maxConcurrentTasks: agent.Spec.MaxConcurrentTasks,
		quota:              agent.Spec.Quota,
	}

	// Apply defaults
	if cfg.agentImage == "" {
		cfg.agentImage = DefaultAgentImage
	}
	if cfg.executorImage == "" {
		cfg.executorImage = DefaultExecutorImage
	}
	if cfg.attachImage == "" {
		cfg.attachImage = DefaultAttachImage
	}

	return cfg
}

// reconcileDeployment ensures the Deployment exists and is up-to-date.
func (r *AgentReconciler) reconcileDeployment(ctx context.Context, agent *kubeopenv1alpha1.Agent, agentCfg agentConfig, sysCfg systemConfig) error {
	logger := log.FromContext(ctx)

	desired := BuildServerDeployment(agent, agentCfg, sysCfg)
	if desired == nil {
		return nil
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(agent, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Check if Deployment exists
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the Deployment
			logger.Info("Creating Deployment for Server-mode Agent", "deployment", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create Deployment: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get Deployment: %w", err)
	}

	// Update the Deployment if needed
	// For now, we do a simple update of the spec
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, &existing); err != nil {
		return fmt.Errorf("failed to update Deployment: %w", err)
	}

	return nil
}

// reconcileService ensures the Service exists and is up-to-date.
func (r *AgentReconciler) reconcileService(ctx context.Context, agent *kubeopenv1alpha1.Agent) error {
	logger := log.FromContext(ctx)

	desired := BuildServerService(agent)
	if desired == nil {
		return nil
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(agent, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Check if Service exists
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the Service
			logger.Info("Creating Service for Server-mode Agent", "service", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create Service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get Service: %w", err)
	}

	// Update the Service if needed
	// Preserve ClusterIP as it's immutable
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, &existing); err != nil {
		return fmt.Errorf("failed to update Service: %w", err)
	}

	return nil
}

// updateAgentStatus updates the Agent's status with server information.
// Health is determined by Deployment readiness (liveness/readiness probes on the Deployment
// already check the server's /session/status endpoint).
func (r *AgentReconciler) updateAgentStatus(ctx context.Context, agent *kubeopenv1alpha1.Agent) error {
	// Get the Deployment to check ready replicas
	var deployment appsv1.Deployment
	deploymentName := ServerDeploymentName(agent.Name)
	err := r.Get(ctx, client.ObjectKey{Namespace: agent.Namespace, Name: deploymentName}, &deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Deployment not found yet, set status to pending
			agent.Status.ServerStatus = &kubeopenv1alpha1.ServerStatus{
				DeploymentName: deploymentName,
				ServiceName:    ServerServiceName(agent.Name),
				URL:            ServerURL(agent.Name, agent.Namespace, GetServerPort(agent)),
				ReadyReplicas:  0,
			}
		} else {
			return fmt.Errorf("failed to get Deployment: %w", err)
		}
	} else {
		// Initialize or update ServerStatus
		if agent.Status.ServerStatus == nil {
			agent.Status.ServerStatus = &kubeopenv1alpha1.ServerStatus{}
		}
		agent.Status.ServerStatus.DeploymentName = deploymentName
		agent.Status.ServerStatus.ServiceName = ServerServiceName(agent.Name)
		agent.Status.ServerStatus.URL = ServerURL(agent.Name, agent.Namespace, GetServerPort(agent))
		agent.Status.ServerStatus.ReadyReplicas = deployment.Status.ReadyReplicas

		// Server health is determined by Deployment readiness
		// The Deployment's readiness probe checks /session/status endpoint
		if deployment.Status.ReadyReplicas > 0 {
			setAgentCondition(agent, AgentConditionServerHealthy, metav1.ConditionTrue, "DeploymentHealthy", "Server deployment has ready replicas")
		}
	}

	// Set ServerReady condition based on ready replicas
	if agent.Status.ServerStatus.ReadyReplicas > 0 {
		setAgentCondition(agent, AgentConditionServerReady, metav1.ConditionTrue, "DeploymentReady", "Server deployment has ready replicas")
	} else {
		setAgentCondition(agent, AgentConditionServerReady, metav1.ConditionFalse, "DeploymentNotReady", "Server deployment has no ready replicas")
	}

	// Update observed generation
	agent.Status.ObservedGeneration = agent.Generation

	// Update the status
	if err := r.Status().Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update Agent status: %w", err)
	}

	return nil
}

// cleanupServerResources removes Deployment and Service if they exist.
// This is called when an Agent is changed from Server-mode to Pod-mode.
func (r *AgentReconciler) cleanupServerResources(ctx context.Context, agent *kubeopenv1alpha1.Agent) error {
	logger := log.FromContext(ctx)

	// Delete Deployment if exists
	deploymentName := ServerDeploymentName(agent.Name)
	var deployment appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: agent.Namespace, Name: deploymentName}, &deployment); err == nil {
		logger.Info("Cleaning up stale Deployment", "deployment", deploymentName)
		if err := r.Delete(ctx, &deployment); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Deployment: %w", err)
		}
	}

	// Delete Service if exists
	serviceName := ServerServiceName(agent.Name)
	var service corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Namespace: agent.Namespace, Name: serviceName}, &service); err == nil {
		logger.Info("Cleaning up stale Service", "service", serviceName)
		if err := r.Delete(ctx, &service); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Service: %w", err)
		}
	}

	// Clear server status if present
	if agent.Status.ServerStatus != nil {
		agent.Status.ServerStatus = nil
		if err := r.Status().Update(ctx, agent); err != nil {
			return fmt.Errorf("failed to clear server status: %w", err)
		}
	}

	return nil
}

// setAgentCondition sets a condition on the Agent.
func setAgentCondition(agent *kubeopenv1alpha1.Agent, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: agent.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Find and update existing condition or append
	for i, c := range agent.Status.Conditions {
		if c.Type == conditionType {
			if c.Status != status {
				agent.Status.Conditions[i] = condition
			}
			return
		}
	}
	agent.Status.Conditions = append(agent.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubeopenv1alpha1.Agent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
