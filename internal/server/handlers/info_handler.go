// Copyright Contributors to the KubeOpenCode project

package handlers

import (
	"context"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubeopencode/kubeopencode/internal/server/types"
)

// Version is set at build time
var Version = "dev"

// InfoHandler handles info-related HTTP requests
type InfoHandler struct {
	defaultClient client.Client
}

// NewInfoHandler creates a new InfoHandler
func NewInfoHandler(c client.Client) *InfoHandler {
	return &InfoHandler{defaultClient: c}
}

// getClient returns the client from context or falls back to default
func (h *InfoHandler) getClient(ctx context.Context) client.Client {
	if c, ok := ctx.Value(clientContextKey{}).(client.Client); ok && c != nil {
		return c
	}
	return h.defaultClient
}

// GetInfo returns server information
func (h *InfoHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, types.ServerInfo{
		Version: Version,
	})
}

// ListNamespaces returns all accessible namespaces
func (h *InfoHandler) ListNamespaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	k8sClient := h.getClient(ctx)

	var nsList corev1.NamespaceList
	if err := k8sClient.List(ctx, &nsList); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list namespaces", err.Error())
		return
	}

	namespaces := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	// Sort namespaces alphabetically
	sort.Strings(namespaces)

	writeJSON(w, http.StatusOK, types.NamespaceList{
		Namespaces: namespaces,
	})
}
