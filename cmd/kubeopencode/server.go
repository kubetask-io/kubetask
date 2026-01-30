// Copyright Contributors to the KubeOpenCode project

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kubeopencode/kubeopencode/internal/server"
)

func init() {
	rootCmd.AddCommand(serverCmd)
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the KubeOpenCode UI server",
	Long: `Start the KubeOpenCode UI server that provides REST API and web interface.

The server exposes:
  - REST API for Task and Agent management
  - Embedded web UI for browser-based access
  - Health and readiness endpoints

Example:
  kubeopencode server --address=:2746`,
	RunE: runServer,
}

// Server flags
var (
	serverAddress       string
	serverBaseURL       string
	serverAuthEnabled   bool
	serverAuthAllowAnon bool
)

func init() {
	serverCmd.Flags().StringVar(&serverAddress, "address", ":2746",
		"The address the server binds to (e.g., :2746 or 0.0.0.0:2746)")
	serverCmd.Flags().StringVar(&serverBaseURL, "base-url", "",
		"Base URL for the UI (e.g., /kubeopencode). Empty means root path.")
	serverCmd.Flags().BoolVar(&serverAuthEnabled, "auth-enabled", false,
		"Enable token-based authentication for API requests")
	serverCmd.Flags().BoolVar(&serverAuthAllowAnon, "auth-allow-anonymous", true,
		"Allow anonymous requests when auth is enabled (for development)")
}

func runServer(cmd *cobra.Command, args []string) error {
	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("server")

	log.Info("Starting KubeOpenCode server", "address", serverAddress)

	// Create server options
	serverOpts := server.Options{
		Address:            serverAddress,
		BaseURL:            serverBaseURL,
		AuthEnabled:        serverAuthEnabled,
		AuthAllowAnonymous: serverAuthAllowAnon,
	}

	// Create the server
	srv, err := server.New(serverOpts)
	if err != nil {
		log.Error(err, "Failed to create server")
		return err
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info("Received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run the server
	if err := srv.Run(ctx); err != nil {
		log.Error(err, "Server error")
		return err
	}

	return nil
}
