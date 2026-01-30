// Copyright Contributors to the KubeOpenCode project

package middleware

import (
	"context"
	"net/http"
	"strings"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("auth")

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// Context keys for user info
const (
	UserInfoKey contextKey = "userInfo"
)

// UserInfo contains authenticated user information
type UserInfo struct {
	Username string
	UID      string
	Groups   []string
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	// Enabled controls whether authentication is enforced
	Enabled bool
	// AllowAnonymous allows unauthenticated requests (for development)
	AllowAnonymous bool
}

// Auth creates an authentication middleware
func Auth(clientset kubernetes.Interface, config AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !config.Enabled {
				// Auth disabled, pass through
				next.ServeHTTP(w, r)
				return
			}

			// Extract Bearer token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if config.AllowAnonymous {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
				return
			}

			token := parts[1]

			// Validate token using TokenReview API
			tokenReview := &authv1.TokenReview{
				Spec: authv1.TokenReviewSpec{
					Token: token,
				},
			}

			result, err := clientset.AuthenticationV1().TokenReviews().Create(
				r.Context(),
				tokenReview,
				metav1.CreateOptions{},
			)
			if err != nil {
				log.Error(err, "Failed to validate token")
				http.Error(w, "Failed to validate token", http.StatusInternalServerError)
				return
			}

			if !result.Status.Authenticated {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Store user info in context
			userInfo := UserInfo{
				Username: result.Status.User.Username,
				UID:      result.Status.User.UID,
				Groups:   result.Status.User.Groups,
			}

			ctx := context.WithValue(r.Context(), UserInfoKey, &userInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserInfo retrieves user info from the request context
func GetUserInfo(ctx context.Context) *UserInfo {
	if userInfo, ok := ctx.Value(UserInfoKey).(*UserInfo); ok {
		return userInfo
	}
	return nil
}
