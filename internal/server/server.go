// Copyright Contributors to the KubeOpenCode project

package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
	"github.com/kubeopencode/kubeopencode/internal/server/handlers"
	authmiddleware "github.com/kubeopencode/kubeopencode/internal/server/middleware"
	"github.com/kubeopencode/kubeopencode/ui"
)

var log = ctrl.Log.WithName("server")

// scheme is the runtime scheme for the server
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kubeopenv1alpha1.AddToScheme(scheme))
}

// Options holds the server configuration
type Options struct {
	// Address is the address the server listens on (e.g., ":2746")
	Address string
	// BaseURL is the base URL path for the UI (e.g., "/kubeopencode")
	BaseURL string
	// AuthEnabled enables token-based authentication
	AuthEnabled bool
	// AuthAllowAnonymous allows unauthenticated requests when auth is enabled
	AuthAllowAnonymous bool
}

// Server is the KubeOpenCode UI server
type Server struct {
	opts       Options
	httpServer *http.Server
	k8sClient  client.Client
	clientset  kubernetes.Interface
	restConfig *rest.Config
}

// New creates a new Server instance
func New(opts Options) (*Server, error) {
	// Create Kubernetes client
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create clientset for authentication
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	s := &Server{
		opts:       opts,
		k8sClient:  k8sClient,
		clientset:  clientset,
		restConfig: cfg,
	}

	return s, nil
}

// Run starts the HTTP server and blocks until the context is cancelled
func (s *Server) Run(ctx context.Context) error {
	router := s.setupRoutes()

	s.httpServer = &http.Server{
		Addr:              s.opts.Address,
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		log.Info("Starting HTTP server", "address", s.opts.Address)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		log.Info("Shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	}
}

// setupRoutes configures the HTTP router
func (s *Server) setupRoutes() *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

	// Health endpoints (no auth required)
	r.Get("/health", s.healthHandler)
	r.Get("/ready", s.readyHandler)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Add authentication middleware for API routes
		authConfig := authmiddleware.AuthConfig{
			Enabled:        s.opts.AuthEnabled,
			AllowAnonymous: s.opts.AuthAllowAnonymous,
		}
		r.Use(authmiddleware.Auth(s.clientset, authConfig))

		// Create handlers with impersonation support
		taskHandler := handlers.NewTaskHandler(s.k8sClient, s.clientset, s.restConfig)
		agentHandler := handlers.NewAgentHandler(s.k8sClient)
		infoHandler := handlers.NewInfoHandler(s.k8sClient)

		// Register impersonation middleware that creates per-request clients
		r.Use(s.impersonationMiddleware)

		// Info endpoints
		r.Get("/info", infoHandler.GetInfo)
		r.Get("/namespaces", infoHandler.ListNamespaces)

		// Task endpoints
		r.Route("/namespaces/{namespace}/tasks", func(r chi.Router) {
			r.Get("/", taskHandler.List)
			r.Post("/", taskHandler.Create)
			r.Get("/{name}", taskHandler.Get)
			r.Delete("/{name}", taskHandler.Delete)
			r.Post("/{name}/stop", taskHandler.Stop)
			r.Get("/{name}/logs", taskHandler.GetLogs)
		})

		// Agent endpoints
		r.Get("/agents", agentHandler.ListAll)
		r.Route("/namespaces/{namespace}/agents", func(r chi.Router) {
			r.Get("/", agentHandler.List)
			r.Get("/{name}", agentHandler.Get)
		})
	})

	// Static UI files (served from root)
	r.Handle("/*", ui.Handler(s.opts.BaseURL))

	return r
}

// healthHandler returns 200 if the server is healthy
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// readyHandler returns 200 if the server is ready to accept requests
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	// Check if we can reach Kubernetes API
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var tasks kubeopenv1alpha1.TaskList
	if err := s.k8sClient.List(ctx, &tasks, client.Limit(1)); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// clientContextKey is the context key for the impersonated client
type clientContextKey struct{}

// impersonationMiddleware creates an impersonated client based on user info
func (s *Server) impersonationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userInfo := authmiddleware.GetUserInfo(r.Context())

		// If no user info (auth disabled or anonymous allowed), use default client
		if userInfo == nil {
			ctx := context.WithValue(r.Context(), clientContextKey{}, s.k8sClient)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Create impersonated config
		impersonatedConfig := rest.CopyConfig(s.restConfig)
		impersonatedConfig.Impersonate = rest.ImpersonationConfig{
			UserName: userInfo.Username,
			UID:      userInfo.UID,
			Groups:   userInfo.Groups,
		}

		// Create impersonated client
		impersonatedClient, err := client.New(impersonatedConfig, client.Options{Scheme: scheme})
		if err != nil {
			log.Error(err, "Failed to create impersonated client", "user", userInfo.Username)
			http.Error(w, "Failed to create client", http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(r.Context(), clientContextKey{}, impersonatedClient)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetClientFromContext retrieves the Kubernetes client from the request context
func GetClientFromContext(ctx context.Context) client.Client {
	if c, ok := ctx.Value(clientContextKey{}).(client.Client); ok {
		return c
	}
	return nil
}
