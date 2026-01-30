// Copyright Contributors to the KubeOpenCode project

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/kubeopencode/kubeopencode/internal/server/types"
)

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, err string, message string) {
	writeJSON(w, status, types.ErrorResponse{
		Error:   err,
		Message: message,
		Code:    status,
	})
}
