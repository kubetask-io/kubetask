// Copyright Contributors to the KubeOpenCode project

package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
	"github.com/kubeopencode/kubeopencode/internal/server/types"
)

// clientContextKey is the context key for the impersonated client
type clientContextKey struct{}

// AgentHandler handles agent-related HTTP requests
type AgentHandler struct {
	defaultClient client.Client
}

// NewAgentHandler creates a new AgentHandler
func NewAgentHandler(c client.Client) *AgentHandler {
	return &AgentHandler{defaultClient: c}
}

// getClient returns the client from context or falls back to default
func (h *AgentHandler) getClient(ctx context.Context) client.Client {
	if c, ok := ctx.Value(clientContextKey{}).(client.Client); ok && c != nil {
		return c
	}
	return h.defaultClient
}

// ListAll returns all agents across all namespaces
func (h *AgentHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var agentList kubeopenv1alpha1.AgentList
	if err := k8sClient.List(ctx, &agentList); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list agents", err.Error())
		return
	}

	response := types.AgentListResponse{
		Agents: make([]types.AgentResponse, 0, len(agentList.Items)),
		Total:  len(agentList.Items),
	}

	for _, agent := range agentList.Items {
		response.Agents = append(response.Agents, agentToResponse(&agent))
	}

	writeJSON(w, http.StatusOK, response)
}

// List returns all agents in a namespace
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var agentList kubeopenv1alpha1.AgentList
	if err := k8sClient.List(ctx, &agentList, client.InNamespace(namespace)); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list agents", err.Error())
		return
	}

	response := types.AgentListResponse{
		Agents: make([]types.AgentResponse, 0, len(agentList.Items)),
		Total:  len(agentList.Items),
	}

	for _, agent := range agentList.Items {
		response.Agents = append(response.Agents, agentToResponse(&agent))
	}

	writeJSON(w, http.StatusOK, response)
}

// Get returns a specific agent
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var agent kubeopenv1alpha1.Agent
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &agent); err != nil {
		writeError(w, http.StatusNotFound, "Agent not found", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(&agent))
}

// agentToResponse converts an Agent CRD to an API response
func agentToResponse(agent *kubeopenv1alpha1.Agent) types.AgentResponse {
	resp := types.AgentResponse{
		Name:              agent.Name,
		Namespace:         agent.Namespace,
		ExecutorImage:     agent.Spec.ExecutorImage,
		AgentImage:        agent.Spec.AgentImage,
		WorkspaceDir:      agent.Spec.WorkspaceDir,
		ContextsCount:     len(agent.Spec.Contexts),
		CredentialsCount:  len(agent.Spec.Credentials),
		AllowedNamespaces: agent.Spec.AllowedNamespaces,
		CreatedAt:         agent.CreationTimestamp.Time,
	}

	if agent.Spec.MaxConcurrentTasks != nil {
		resp.MaxConcurrentTasks = agent.Spec.MaxConcurrentTasks
	}

	if agent.Spec.Quota != nil {
		resp.Quota = &types.QuotaInfo{
			MaxTaskStarts: agent.Spec.Quota.MaxTaskStarts,
			WindowSeconds: agent.Spec.Quota.WindowSeconds,
		}
	}

	// Add credential info (without exposing secrets)
	for _, cred := range agent.Spec.Credentials {
		credInfo := types.CredentialInfo{
			Name:      cred.Name,
			SecretRef: cred.SecretRef.Name,
		}
		if cred.MountPath != nil {
			credInfo.MountPath = *cred.MountPath
		}
		if cred.Env != nil {
			credInfo.Env = *cred.Env
		}
		resp.Credentials = append(resp.Credentials, credInfo)
	}

	// Add context info
	for _, ctx := range agent.Spec.Contexts {
		ctxItem := types.ContextItem{
			Name:        ctx.Name,
			Description: ctx.Description,
			Type:        string(ctx.Type),
			MountPath:   ctx.MountPath,
		}
		resp.Contexts = append(resp.Contexts, ctxItem)
	}

	return resp
}
