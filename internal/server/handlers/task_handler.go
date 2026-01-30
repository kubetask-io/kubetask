// Copyright Contributors to the KubeOpenCode project

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
	"github.com/kubeopencode/kubeopencode/internal/server/types"
)

// TaskHandler handles task-related HTTP requests
type TaskHandler struct {
	defaultClient    client.Client
	defaultClientset kubernetes.Interface
	restConfig       *rest.Config
}

// NewTaskHandler creates a new TaskHandler
func NewTaskHandler(c client.Client, clientset kubernetes.Interface, restConfig *rest.Config) *TaskHandler {
	return &TaskHandler{
		defaultClient:    c,
		defaultClientset: clientset,
		restConfig:       restConfig,
	}
}

// getClient returns the client from context or falls back to default
func (h *TaskHandler) getClient(ctx context.Context) client.Client {
	if c, ok := ctx.Value(clientContextKey{}).(client.Client); ok && c != nil {
		return c
	}
	return h.defaultClient
}

// List returns all tasks in a namespace
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var taskList kubeopenv1alpha1.TaskList
	if err := k8sClient.List(ctx, &taskList, client.InNamespace(namespace)); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list tasks", err.Error())
		return
	}

	response := types.TaskListResponse{
		Tasks: make([]types.TaskResponse, 0, len(taskList.Items)),
		Total: len(taskList.Items),
	}

	for _, task := range taskList.Items {
		response.Tasks = append(response.Tasks, taskToResponse(&task))
	}

	writeJSON(w, http.StatusOK, response)
}

// Get returns a specific task
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var task kubeopenv1alpha1.Task
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &task); err != nil {
		writeError(w, http.StatusNotFound, "Task not found", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(&task))
}

// Create creates a new task
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var req types.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	if req.Description == "" {
		writeError(w, http.StatusBadRequest, "Description is required", "")
		return
	}

	task := &kubeopenv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: kubeopenv1alpha1.TaskSpec{
			Description: &req.Description,
		},
	}

	// Set name or generate name
	if req.Name != "" {
		task.ObjectMeta.Name = req.Name
	} else {
		task.ObjectMeta.GenerateName = "task-"
	}

	// Set agent reference if provided
	if req.AgentRef != nil {
		task.Spec.AgentRef = &kubeopenv1alpha1.AgentReference{
			Name:      req.AgentRef.Name,
			Namespace: req.AgentRef.Namespace,
		}
	}

	// Convert contexts
	for _, c := range req.Contexts {
		item := kubeopenv1alpha1.ContextItem{
			Name:        c.Name,
			Description: c.Description,
			MountPath:   c.MountPath,
		}
		switch c.Type {
		case "Text":
			item.Type = kubeopenv1alpha1.ContextTypeText
			item.Text = c.Text
		}
		task.Spec.Contexts = append(task.Spec.Contexts, item)
	}

	if err := k8sClient.Create(ctx, task); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create task", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, taskToResponse(task))
}

// Delete deletes a task
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var task kubeopenv1alpha1.Task
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &task); err != nil {
		writeError(w, http.StatusNotFound, "Task not found", err.Error())
		return
	}

	if err := k8sClient.Delete(ctx, &task); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete task", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Stop stops a running task by adding the stop annotation
func (h *TaskHandler) Stop(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var task kubeopenv1alpha1.Task
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &task); err != nil {
		writeError(w, http.StatusNotFound, "Task not found", err.Error())
		return
	}

	// Check if task is running
	if task.Status.Phase != kubeopenv1alpha1.TaskPhaseRunning {
		writeError(w, http.StatusBadRequest, "Task is not running", fmt.Sprintf("Task phase is %s", task.Status.Phase))
		return
	}

	// Add stop annotation
	if task.Annotations == nil {
		task.Annotations = make(map[string]string)
	}
	task.Annotations["kubeopencode.io/stop"] = "true"

	if err := k8sClient.Update(ctx, &task); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to stop task", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(&task))
}

// GetLogs streams task logs via Server-Sent Events
func (h *TaskHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	// Check if follow mode is requested (default: true for SSE)
	follow := r.URL.Query().Get("follow") != "false"
	// Container name (default: agent)
	container := r.URL.Query().Get("container")
	if container == "" {
		container = "agent"
	}

	var task kubeopenv1alpha1.Task
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &task); err != nil {
		writeError(w, http.StatusNotFound, "Task not found", err.Error())
		return
	}

	if task.Status.PodName == "" {
		writeError(w, http.StatusBadRequest, "Task has no pod", "Pod not yet created")
		return
	}

	// Get pod namespace (might be different from task namespace for cross-namespace agents)
	podNamespace := task.Status.PodNamespace
	if podNamespace == "" {
		podNamespace = namespace
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported", "")
		return
	}

	// Check if pod exists
	var pod corev1.Pod
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: podNamespace, Name: task.Status.PodName}, &pod); err != nil {
		fmt.Fprintf(w, "data: {\"type\": \"error\", \"message\": \"Pod not found: %s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Send initial status
	fmt.Fprintf(w, "data: {\"type\": \"status\", \"phase\": \"%s\", \"podPhase\": \"%s\"}\n\n", task.Status.Phase, pod.Status.Phase)
	flusher.Flush()

	// Stream pod logs using clientset
	h.streamPodLogs(ctx, w, flusher, podNamespace, task.Status.PodName, container, follow, namespace, name)
}

// streamPodLogs streams actual pod logs using clientset
func (h *TaskHandler) streamPodLogs(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, podNamespace, podName, container string, follow bool, taskNamespace, taskName string) {
	// Create pod log options
	logOptions := &corev1.PodLogOptions{
		Container: container,
		Follow:    follow,
	}

	// Get log stream from clientset
	req := h.defaultClientset.CoreV1().Pods(podNamespace).GetLogs(podName, logOptions)
	stream, err := req.Stream(ctx)
	if err != nil {
		// If container not found or not ready, try without specifying container
		logOptions.Container = ""
		req = h.defaultClientset.CoreV1().Pods(podNamespace).GetLogs(podName, logOptions)
		stream, err = req.Stream(ctx)
		if err != nil {
			fmt.Fprintf(w, "data: {\"type\": \"error\", \"message\": \"Failed to get logs: %s\"}\n\n", err.Error())
			flusher.Flush()
			return
		}
	}
	defer stream.Close()

	// Read logs line by line and send as SSE events
	reader := bufio.NewReader(stream)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					// Check if task is completed
					k8sClient := h.getClient(ctx)
					var task kubeopenv1alpha1.Task
					if getErr := k8sClient.Get(ctx, client.ObjectKey{Namespace: taskNamespace, Name: taskName}, &task); getErr == nil {
						fmt.Fprintf(w, "data: {\"type\": \"complete\", \"phase\": \"%s\"}\n\n", task.Status.Phase)
					} else {
						fmt.Fprintf(w, "data: {\"type\": \"complete\", \"phase\": \"Unknown\"}\n\n")
					}
					flusher.Flush()
					return
				}
				fmt.Fprintf(w, "data: {\"type\": \"error\", \"message\": \"Read error: %s\"}\n\n", err.Error())
				flusher.Flush()
				return
			}

			// Send log line as SSE event
			// Escape the log content for JSON
			logContent := string(line)
			escapedContent, _ := json.Marshal(logContent)
			fmt.Fprintf(w, "data: {\"type\": \"log\", \"content\": %s}\n\n", escapedContent)
			flusher.Flush()
		}
	}
}

// taskToResponse converts a Task CRD to an API response
func taskToResponse(task *kubeopenv1alpha1.Task) types.TaskResponse {
	var description string
	if task.Spec.Description != nil {
		description = *task.Spec.Description
	}

	resp := types.TaskResponse{
		Name:         task.Name,
		Namespace:    task.Namespace,
		Phase:        string(task.Status.Phase),
		Description:  description,
		PodName:      task.Status.PodName,
		PodNamespace: task.Status.PodNamespace,
		CreatedAt:    task.CreationTimestamp.Time,
	}

	if task.Spec.AgentRef != nil {
		resp.AgentRef = &types.AgentReference{
			Name:      task.Spec.AgentRef.Name,
			Namespace: task.Spec.AgentRef.Namespace,
		}
	}

	// Use resolved agent ref from status if available
	if task.Status.AgentRef != nil {
		resp.AgentRef = &types.AgentReference{
			Name:      task.Status.AgentRef.Name,
			Namespace: task.Status.AgentRef.Namespace,
		}
	}

	if task.Status.StartTime != nil {
		t := task.Status.StartTime.Time
		resp.StartTime = &t
	}

	if task.Status.CompletionTime != nil {
		t := task.Status.CompletionTime.Time
		resp.CompletionTime = &t
	}

	// Calculate duration
	if resp.StartTime != nil {
		endTime := time.Now()
		if resp.CompletionTime != nil {
			endTime = *resp.CompletionTime
		}
		resp.Duration = endTime.Sub(*resp.StartTime).Round(time.Second).String()
	}

	// Convert conditions
	for _, c := range task.Status.Conditions {
		resp.Conditions = append(resp.Conditions, types.Condition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	return resp
}
