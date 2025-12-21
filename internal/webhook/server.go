// Copyright Contributors to the KubeTask project

// Package webhook provides an HTTP server for receiving webhooks and creating Tasks.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

// Server is an HTTP server that handles webhook requests and creates Tasks.
type Server struct {
	client client.Client
	log    logr.Logger

	// triggers maps webhook paths to their configurations
	// key format: "<namespace>/<name>"
	triggers map[string]*kubetaskv1alpha1.WebhookTrigger
	mu       sync.RWMutex

	// celFilter evaluates CEL expressions for webhook filtering
	celFilter *CELFilter

	// httpServer is the underlying HTTP server
	httpServer *http.Server
}

// NewServer creates a new webhook server.
func NewServer(c client.Client, log logr.Logger) *Server {
	return &Server{
		client:    c,
		log:       log.WithName("webhook-server"),
		triggers:  make(map[string]*kubetaskv1alpha1.WebhookTrigger),
		celFilter: NewCELFilter(),
	}
}

// RegisterTrigger registers or updates a WebhookTrigger.
func (s *Server) RegisterTrigger(trigger *kubetaskv1alpha1.WebhookTrigger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s/%s", trigger.Namespace, trigger.Name)
	s.triggers[key] = trigger.DeepCopy()
	s.log.Info("Registered webhook trigger", "key", key)
}

// UnregisterTrigger removes a WebhookTrigger.
func (s *Server) UnregisterTrigger(namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	delete(s.triggers, key)
	s.log.Info("Unregistered webhook trigger", "key", key)
}

// GetTrigger retrieves a WebhookTrigger by namespace and name.
func (s *Server) GetTrigger(namespace, name string) (*kubetaskv1alpha1.WebhookTrigger, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	trigger, ok := s.triggers[key]
	if !ok {
		return nil, false
	}
	return trigger.DeepCopy(), true
}

// Start starts the HTTP server on the specified port.
func (s *Server) Start(ctx context.Context, port int) error {
	mux := http.NewServeMux()

	// Main webhook handler
	mux.HandleFunc("/webhooks/", s.handleWebhook)

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Ready check endpoint
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.log.Info("Starting webhook server", "port", port)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.log.Info("Shutting down webhook server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// CreatedResource represents a resource created by the webhook handler.
type CreatedResource struct {
	Kind      string // "Task" or "WorkflowRun"
	Name      string
	Namespace string
	RuleName  string // Empty for legacy single-rule triggers
}

// handleWebhook processes incoming webhook requests.
// Expected URL format: /webhooks/<namespace>/<trigger-name>
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /webhooks/<namespace>/<name>
	// Path should be at least "/webhooks/ns/name" = 3 parts after split
	path := r.URL.Path
	if len(path) < len("/webhooks/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Remove "/webhooks/" prefix and split
	remaining := path[len("/webhooks/"):]
	var namespace, name string
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == '/' {
			namespace = remaining[:i]
			name = remaining[i+1:]
			break
		}
	}

	if namespace == "" || name == "" {
		http.Error(w, "Invalid webhook path, expected /webhooks/<namespace>/<name>", http.StatusBadRequest)
		return
	}

	log := s.log.WithValues("namespace", namespace, "name", name)

	// Get trigger configuration
	trigger, ok := s.GetTrigger(namespace, name)
	if !ok {
		log.Info("Webhook trigger not found")
		http.Error(w, "Webhook trigger not found", http.StatusNotFound)
		return
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		log.Error(err, "Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Validate authentication
	if trigger.Spec.Auth != nil {
		if err := s.validateAuth(r, body, trigger.Spec.Auth, namespace); err != nil {
			log.Info("Authentication failed", "error", err.Error())
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
	}

	// Parse payload
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Error(err, "Failed to parse JSON payload")
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check if using rules-based approach or legacy single-rule
	if len(trigger.Spec.Rules) > 0 {
		s.handleRulesBasedTrigger(ctx, w, log, trigger, payload, r.Header, namespace)
		return
	}

	// Legacy: single filter + taskTemplate/resourceTemplate
	s.handleLegacyTrigger(ctx, w, log, trigger, payload, r.Header, namespace)
}

// handleRulesBasedTrigger processes webhooks using the new rules-based approach.
func (s *Server) handleRulesBasedTrigger(ctx context.Context, w http.ResponseWriter, log logr.Logger, trigger *kubetaskv1alpha1.WebhookTrigger, payload map[string]interface{}, headers http.Header, namespace string) {
	matchPolicy := trigger.Spec.MatchPolicy
	if matchPolicy == "" {
		matchPolicy = kubetaskv1alpha1.MatchPolicyFirst
	}

	var matchedRules []kubetaskv1alpha1.WebhookRule
	var createdResources []CreatedResource
	var skippedRules []string

	// Evaluate rules in order
	for _, rule := range trigger.Spec.Rules {
		match, err := s.celFilter.Evaluate(rule.Filter, payload, headers)
		if err != nil {
			log.Error(err, "Failed to evaluate CEL filter for rule", "rule", rule.Name)
			continue
		}

		if match {
			matchedRules = append(matchedRules, rule)

			if matchPolicy == kubetaskv1alpha1.MatchPolicyFirst {
				// First match: stop evaluating
				break
			}
		}
	}

	if len(matchedRules) == 0 {
		log.Info("Webhook did not match any rules")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "filtered", "matchedRules": 0}`))
		return
	}

	// Process matched rules
	for _, rule := range matchedRules {
		// Determine concurrency policy (rule-level overrides trigger-level)
		policy := rule.ConcurrencyPolicy
		if policy == "" {
			policy = trigger.Spec.ConcurrencyPolicy
		}
		if policy == "" {
			policy = kubetaskv1alpha1.ConcurrencyPolicyAllow
		}

		// Handle concurrency for this rule
		skipped, err := s.handleRuleConcurrency(ctx, trigger, rule.Name, policy, namespace)
		if err != nil {
			log.Error(err, "Failed to handle concurrency for rule", "rule", rule.Name)
			continue
		}
		if skipped {
			log.Info("Rule skipped due to concurrency policy", "rule", rule.Name)
			skippedRules = append(skippedRules, rule.Name)
			continue
		}

		// Create resource from rule template
		resource, err := s.createResourceFromTemplate(ctx, trigger, &rule.ResourceTemplate, rule.Name, payload, headers, namespace)
		if err != nil {
			log.Error(err, "Failed to create resource for rule", "rule", rule.Name)
			continue
		}

		log.Info("Created resource from webhook rule",
			"rule", rule.Name,
			"kind", resource.Kind,
			"name", resource.Name)

		createdResources = append(createdResources, *resource)
	}

	// Update trigger status with all created resources
	if len(createdResources) > 0 {
		if err := s.updateTriggerStatusWithRules(ctx, trigger, createdResources); err != nil {
			log.Error(err, "Failed to update trigger status")
		}
	}

	// Return response
	s.writeRulesResponse(w, createdResources, skippedRules, namespace)
}

// handleLegacyTrigger processes webhooks using the legacy single-rule approach.
func (s *Server) handleLegacyTrigger(ctx context.Context, w http.ResponseWriter, log logr.Logger, trigger *kubetaskv1alpha1.WebhookTrigger, payload map[string]interface{}, headers http.Header, namespace string) {
	// Apply CEL filter
	match, err := s.celFilter.Evaluate(trigger.Spec.Filter, payload, headers)
	if err != nil {
		log.Error(err, "Failed to evaluate CEL filter")
		http.Error(w, "Filter evaluation error", http.StatusBadRequest)
		return
	}
	if !match {
		log.Info("Webhook did not match filter")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "filtered"}`))
		return
	}

	// Handle concurrency policy
	if err := s.handleConcurrency(ctx, trigger, namespace); err != nil {
		if err == errConcurrencySkipped {
			log.Info("Webhook skipped due to concurrency policy")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status": "skipped", "reason": "concurrency_policy"}`))
			return
		}
		log.Error(err, "Failed to handle concurrency")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Create resource based on template type
	var resource *CreatedResource

	// Check for new ResourceTemplate first
	if trigger.Spec.ResourceTemplate != nil {
		resource, err = s.createResourceFromTemplate(ctx, trigger, trigger.Spec.ResourceTemplate, "", payload, headers, namespace)
	} else if trigger.Spec.TaskTemplate != nil {
		// Legacy TaskTemplate
		task, taskErr := s.createTaskFromLegacyTemplate(ctx, trigger, payload, namespace)
		if taskErr != nil {
			err = taskErr
		} else {
			resource = &CreatedResource{
				Kind:      "Task",
				Name:      task.Name,
				Namespace: task.Namespace,
			}
		}
	} else {
		http.Error(w, "No taskTemplate or resourceTemplate specified", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Error(err, "Failed to create resource")
		http.Error(w, "Failed to create resource", http.StatusInternalServerError)
		return
	}

	log.Info("Created resource from webhook", "kind", resource.Kind, "name", resource.Name)

	// Update trigger status
	if err := s.updateTriggerStatus(ctx, trigger, resource.Name); err != nil {
		log.Error(err, "Failed to update trigger status")
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "created",
		"kind":      resource.Kind,
		"name":      resource.Name,
		"namespace": resource.Namespace,
	})
}

// writeRulesResponse writes the HTTP response for rules-based triggers.
func (s *Server) writeRulesResponse(w http.ResponseWriter, created []CreatedResource, skipped []string, namespace string) {
	w.Header().Set("Content-Type", "application/json")

	if len(created) == 0 {
		// All rules were skipped
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "skipped",
			"skippedRules": skipped,
		})
		return
	}

	// Build response
	status := "created"
	if len(skipped) > 0 {
		status = "partial"
	}

	resources := make([]map[string]string, len(created))
	for i, r := range created {
		resources[i] = map[string]string{
			"rule": r.RuleName,
			"kind": r.Kind,
			"name": r.Name,
		}
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       status,
		"resources":    resources,
		"namespace":    namespace,
		"skippedRules": skipped,
	})
}

var errConcurrencySkipped = fmt.Errorf("skipped due to concurrency policy")

// handleConcurrency applies the concurrency policy.
func (s *Server) handleConcurrency(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, namespace string) error {
	policy := trigger.Spec.ConcurrencyPolicy
	if policy == "" {
		policy = kubetaskv1alpha1.ConcurrencyPolicyAllow
	}

	if policy == kubetaskv1alpha1.ConcurrencyPolicyAllow {
		return nil
	}

	// Query active tasks directly from cluster for accurate state
	activeTasks, err := s.getActiveTasks(ctx, namespace, trigger.Name)
	if err != nil {
		return fmt.Errorf("failed to get active tasks: %w", err)
	}

	switch policy {
	case kubetaskv1alpha1.ConcurrencyPolicyForbid:
		// Check if there are active tasks
		if len(activeTasks) > 0 {
			return errConcurrencySkipped
		}
		return nil

	case kubetaskv1alpha1.ConcurrencyPolicyReplace:
		// Stop all active tasks
		for _, taskName := range activeTasks {
			if err := s.stopTask(ctx, namespace, taskName); err != nil {
				s.log.Error(err, "Failed to stop task", "task", taskName)
				// Continue trying to stop other tasks
			}
		}
		return nil
	}

	return nil
}

// getActiveTasks returns the list of active tasks for a trigger by querying directly.
func (s *Server) getActiveTasks(ctx context.Context, namespace, triggerName string) ([]string, error) {
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := s.client.List(ctx, taskList,
		client.InNamespace(namespace),
		client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName},
	); err != nil {
		return nil, err
	}

	var activeTasks []string
	for _, task := range taskList.Items {
		// Include tasks that are still active (not completed or failed)
		if task.Status.Phase != kubetaskv1alpha1.TaskPhaseCompleted &&
			task.Status.Phase != kubetaskv1alpha1.TaskPhaseFailed {
			activeTasks = append(activeTasks, task.Name)
		}
	}

	return activeTasks, nil
}

// stopTask stops a running task by adding the stop annotation.
func (s *Server) stopTask(ctx context.Context, namespace, name string) error {
	task := &kubetaskv1alpha1.Task{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, task); err != nil {
		return err
	}

	// Check if task is already completed/failed
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == kubetaskv1alpha1.TaskPhaseFailed {
		return nil
	}

	// Add stop annotation
	if task.Annotations == nil {
		task.Annotations = make(map[string]string)
	}
	task.Annotations["kubetask.io/stop"] = "true"

	return s.client.Update(ctx, task)
}

// createTask creates a new Task from the webhook trigger template.
func (s *Server) createTask(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, payload map[string]interface{}, namespace string) (*kubetaskv1alpha1.Task, error) {
	// Render description template
	description, err := RenderTemplate(trigger.Spec.TaskTemplate.Description, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to render description template: %w", err)
	}

	// Create Task
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", trigger.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				"kubetask.io/webhook-trigger": trigger.Name,
			},
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			Description: &description,
			AgentRef:    trigger.Spec.TaskTemplate.AgentRef,
			Contexts:    trigger.Spec.TaskTemplate.Contexts,
		},
	}

	if err := s.client.Create(ctx, task); err != nil {
		return nil, err
	}

	return task, nil
}

// updateTriggerStatus updates the WebhookTrigger status after creating a task.
func (s *Server) updateTriggerStatus(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, resourceName string) error {
	// Get fresh trigger
	currentTrigger := &kubetaskv1alpha1.WebhookTrigger{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: trigger.Namespace, Name: trigger.Name}, currentTrigger); err != nil {
		return err
	}

	// Update status
	now := metav1.Now()
	currentTrigger.Status.LastTriggeredTime = &now
	currentTrigger.Status.TotalTriggered++
	// Keep ActiveTasks for backward compatibility
	currentTrigger.Status.ActiveTasks = append(currentTrigger.Status.ActiveTasks, resourceName)
	// Also update ActiveResources for new code
	currentTrigger.Status.ActiveResources = append(currentTrigger.Status.ActiveResources, resourceName)

	return s.client.Status().Update(ctx, currentTrigger)
}

// handleRuleConcurrency handles concurrency for a specific rule.
// Returns true if the rule should be skipped due to concurrency policy.
func (s *Server) handleRuleConcurrency(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, ruleName string, policy kubetaskv1alpha1.ConcurrencyPolicy, namespace string) (bool, error) {
	if policy == kubetaskv1alpha1.ConcurrencyPolicyAllow {
		return false, nil
	}

	// Query active resources for this specific rule
	activeResources, err := s.getActiveResourcesForRule(ctx, namespace, trigger.Name, ruleName)
	if err != nil {
		return false, fmt.Errorf("failed to get active resources: %w", err)
	}

	switch policy {
	case kubetaskv1alpha1.ConcurrencyPolicyForbid:
		if len(activeResources) > 0 {
			return true, nil // Skip
		}
		return false, nil

	case kubetaskv1alpha1.ConcurrencyPolicyReplace:
		// Stop all active resources for this rule
		for _, resourceName := range activeResources {
			if err := s.stopResource(ctx, namespace, resourceName); err != nil {
				s.log.Error(err, "Failed to stop resource", "resource", resourceName, "rule", ruleName)
			}
		}
		return false, nil
	}

	return false, nil
}

// getActiveResourcesForRule returns active resources for a specific rule.
func (s *Server) getActiveResourcesForRule(ctx context.Context, namespace, triggerName, ruleName string) ([]string, error) {
	var activeResources []string

	// Check Tasks
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := s.client.List(ctx, taskList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"kubetask.io/webhook-trigger": triggerName,
			"kubetask.io/webhook-rule":    ruleName,
		},
	); err != nil {
		return nil, err
	}

	for _, task := range taskList.Items {
		if task.Status.Phase != kubetaskv1alpha1.TaskPhaseCompleted &&
			task.Status.Phase != kubetaskv1alpha1.TaskPhaseFailed {
			activeResources = append(activeResources, task.Name)
		}
	}

	// Check WorkflowRuns
	workflowRunList := &kubetaskv1alpha1.WorkflowRunList{}
	if err := s.client.List(ctx, workflowRunList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"kubetask.io/webhook-trigger": triggerName,
			"kubetask.io/webhook-rule":    ruleName,
		},
	); err != nil {
		return nil, err
	}

	for _, wr := range workflowRunList.Items {
		if wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseCompleted &&
			wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseFailed {
			activeResources = append(activeResources, wr.Name)
		}
	}

	return activeResources, nil
}

// stopResource stops a running Task or WorkflowRun by adding the stop annotation.
func (s *Server) stopResource(ctx context.Context, namespace, name string) error {
	// Try Task first
	task := &kubetaskv1alpha1.Task{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, task); err == nil {
		if task.Status.Phase != kubetaskv1alpha1.TaskPhaseCompleted &&
			task.Status.Phase != kubetaskv1alpha1.TaskPhaseFailed {
			if task.Annotations == nil {
				task.Annotations = make(map[string]string)
			}
			task.Annotations["kubetask.io/stop"] = "true"
			return s.client.Update(ctx, task)
		}
		return nil
	}

	// Try WorkflowRun
	wr := &kubetaskv1alpha1.WorkflowRun{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, wr); err == nil {
		if wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseCompleted &&
			wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseFailed {
			if wr.Annotations == nil {
				wr.Annotations = make(map[string]string)
			}
			wr.Annotations["kubetask.io/stop"] = "true"
			return s.client.Update(ctx, wr)
		}
		return nil
	}

	return nil
}

// createResourceFromTemplate creates a Task or WorkflowRun from a resource template.
func (s *Server) createResourceFromTemplate(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, template *kubetaskv1alpha1.WebhookResourceTemplate, ruleName string, payload map[string]interface{}, headers http.Header, namespace string) (*CreatedResource, error) {
	// Add headers to payload for template rendering
	templateData := s.buildTemplateData(payload, headers)

	// Determine which resource type to create
	if template.Task != nil {
		return s.createTaskFromSpec(ctx, trigger, template.Task, ruleName, templateData, namespace)
	}

	if template.WorkflowRef != "" {
		return s.createWorkflowRunFromRef(ctx, trigger, template.WorkflowRef, ruleName, namespace)
	}

	if template.WorkflowRun != nil {
		return s.createWorkflowRunFromSpec(ctx, trigger, template.WorkflowRun, ruleName, templateData, namespace)
	}

	return nil, fmt.Errorf("resourceTemplate must specify one of: task, workflowRef, or workflowRun")
}

// buildTemplateData creates the data map for template rendering.
func (s *Server) buildTemplateData(payload map[string]interface{}, headers http.Header) map[string]interface{} {
	data := make(map[string]interface{})
	for k, v := range payload {
		data[k] = v
	}

	// Add headers as lowercase keys
	headerMap := make(map[string]string)
	for k, v := range headers {
		if len(v) > 0 {
			headerMap[strings.ToLower(k)] = v[0]
		}
	}
	data["headers"] = headerMap

	return data
}

// createTaskFromSpec creates a Task from WebhookTaskSpec.
func (s *Server) createTaskFromSpec(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, spec *kubetaskv1alpha1.WebhookTaskSpec, ruleName string, templateData map[string]interface{}, namespace string) (*CreatedResource, error) {
	description, err := RenderTemplate(spec.Description, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to render description template: %w", err)
	}

	// Build name prefix
	namePrefix := trigger.Name
	if ruleName != "" {
		namePrefix = fmt.Sprintf("%s-%s", trigger.Name, ruleName)
	}

	// Build labels
	labels := map[string]string{
		"kubetask.io/webhook-trigger": trigger.Name,
		"kubetask.io/resource-kind":   "Task",
	}
	if ruleName != "" {
		labels["kubetask.io/webhook-rule"] = ruleName
	}

	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", namePrefix),
			Namespace:    namespace,
			Labels:       labels,
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			Description: &description,
			AgentRef:    spec.AgentRef,
			Contexts:    spec.Contexts,
		},
	}

	if err := s.client.Create(ctx, task); err != nil {
		return nil, err
	}

	return &CreatedResource{
		Kind:      "Task",
		Name:      task.Name,
		Namespace: task.Namespace,
		RuleName:  ruleName,
	}, nil
}

// createWorkflowRunFromRef creates a WorkflowRun that references an existing Workflow.
func (s *Server) createWorkflowRunFromRef(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, workflowRef string, ruleName string, namespace string) (*CreatedResource, error) {
	// Build name prefix
	namePrefix := trigger.Name
	if ruleName != "" {
		namePrefix = fmt.Sprintf("%s-%s", trigger.Name, ruleName)
	}

	// Build labels
	labels := map[string]string{
		"kubetask.io/webhook-trigger": trigger.Name,
		"kubetask.io/resource-kind":   "WorkflowRun",
	}
	if ruleName != "" {
		labels["kubetask.io/webhook-rule"] = ruleName
	}

	wr := &kubetaskv1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", namePrefix),
			Namespace:    namespace,
			Labels:       labels,
		},
		Spec: kubetaskv1alpha1.WorkflowRunSpec{
			WorkflowRef: workflowRef,
		},
	}

	if err := s.client.Create(ctx, wr); err != nil {
		return nil, err
	}

	return &CreatedResource{
		Kind:      "WorkflowRun",
		Name:      wr.Name,
		Namespace: wr.Namespace,
		RuleName:  ruleName,
	}, nil
}

// createWorkflowRunFromSpec creates a WorkflowRun with inline or ref spec.
func (s *Server) createWorkflowRunFromSpec(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, spec *kubetaskv1alpha1.WebhookWorkflowRunSpec, ruleName string, templateData map[string]interface{}, namespace string) (*CreatedResource, error) {
	// Build name prefix
	namePrefix := trigger.Name
	if ruleName != "" {
		namePrefix = fmt.Sprintf("%s-%s", trigger.Name, ruleName)
	}

	// Build labels
	labels := map[string]string{
		"kubetask.io/webhook-trigger": trigger.Name,
		"kubetask.io/resource-kind":   "WorkflowRun",
	}
	if ruleName != "" {
		labels["kubetask.io/webhook-rule"] = ruleName
	}

	wr := &kubetaskv1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", namePrefix),
			Namespace:    namespace,
			Labels:       labels,
		},
	}

	// Check if using workflowRef or inline
	if spec.WorkflowRef != "" {
		wr.Spec.WorkflowRef = spec.WorkflowRef
	} else if spec.Inline != nil {
		// Render templates in inline workflow descriptions
		renderedInline, err := s.renderWorkflowSpec(spec.Inline, templateData)
		if err != nil {
			return nil, fmt.Errorf("failed to render inline workflow: %w", err)
		}
		wr.Spec.Inline = renderedInline
	} else {
		return nil, fmt.Errorf("workflowRun spec must specify workflowRef or inline")
	}

	if err := s.client.Create(ctx, wr); err != nil {
		return nil, err
	}

	return &CreatedResource{
		Kind:      "WorkflowRun",
		Name:      wr.Name,
		Namespace: wr.Namespace,
		RuleName:  ruleName,
	}, nil
}

// renderWorkflowSpec renders templates in a WorkflowSpec.
func (s *Server) renderWorkflowSpec(spec *kubetaskv1alpha1.WorkflowSpec, templateData map[string]interface{}) (*kubetaskv1alpha1.WorkflowSpec, error) {
	rendered := spec.DeepCopy()

	for i := range rendered.Stages {
		for j := range rendered.Stages[i].Tasks {
			// WorkflowTask has Name and Spec (TaskSpec), Description is in Spec
			if rendered.Stages[i].Tasks[j].Spec.Description != nil && *rendered.Stages[i].Tasks[j].Spec.Description != "" {
				desc, err := RenderTemplate(*rendered.Stages[i].Tasks[j].Spec.Description, templateData)
				if err != nil {
					return nil, err
				}
				rendered.Stages[i].Tasks[j].Spec.Description = &desc
			}
		}
	}

	return rendered, nil
}

// createTaskFromLegacyTemplate creates a Task from the legacy TaskTemplate.
func (s *Server) createTaskFromLegacyTemplate(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, payload map[string]interface{}, namespace string) (*kubetaskv1alpha1.Task, error) {
	if trigger.Spec.TaskTemplate == nil {
		return nil, fmt.Errorf("taskTemplate is nil")
	}

	description, err := RenderTemplate(trigger.Spec.TaskTemplate.Description, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to render description template: %w", err)
	}

	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", trigger.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				"kubetask.io/webhook-trigger": trigger.Name,
				"kubetask.io/resource-kind":   "Task",
			},
		},
		Spec: kubetaskv1alpha1.TaskSpec{
			Description: &description,
			AgentRef:    trigger.Spec.TaskTemplate.AgentRef,
			Contexts:    trigger.Spec.TaskTemplate.Contexts,
		},
	}

	if err := s.client.Create(ctx, task); err != nil {
		return nil, err
	}

	return task, nil
}

// updateTriggerStatusWithRules updates the WebhookTrigger status for rules-based triggers.
func (s *Server) updateTriggerStatusWithRules(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, resources []CreatedResource) error {
	// Get fresh trigger
	currentTrigger := &kubetaskv1alpha1.WebhookTrigger{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: trigger.Namespace, Name: trigger.Name}, currentTrigger); err != nil {
		return err
	}

	now := metav1.Now()
	currentTrigger.Status.LastTriggeredTime = &now
	currentTrigger.Status.TotalTriggered += int64(len(resources))

	// Update global active resources
	for _, r := range resources {
		currentTrigger.Status.ActiveResources = append(currentTrigger.Status.ActiveResources, r.Name)
		// Also update ActiveTasks for backward compatibility
		currentTrigger.Status.ActiveTasks = append(currentTrigger.Status.ActiveTasks, r.Name)
	}

	// Update per-rule statuses
	ruleStatusMap := make(map[string]*kubetaskv1alpha1.WebhookRuleStatus)
	for i := range currentTrigger.Status.RuleStatuses {
		ruleStatusMap[currentTrigger.Status.RuleStatuses[i].Name] = &currentTrigger.Status.RuleStatuses[i]
	}

	for _, r := range resources {
		if r.RuleName == "" {
			continue
		}

		status, ok := ruleStatusMap[r.RuleName]
		if !ok {
			currentTrigger.Status.RuleStatuses = append(currentTrigger.Status.RuleStatuses, kubetaskv1alpha1.WebhookRuleStatus{
				Name:              r.RuleName,
				LastTriggeredTime: &now,
				TotalTriggered:    1,
				ActiveResources:   []string{r.Name},
			})
		} else {
			status.LastTriggeredTime = &now
			status.TotalTriggered++
			status.ActiveResources = append(status.ActiveResources, r.Name)
		}
	}

	return s.client.Status().Update(ctx, currentTrigger)
}
