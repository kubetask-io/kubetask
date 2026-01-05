// Copyright Contributors to the KubeOpenCode project

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// Environment variable names for url-fetch
const (
	envURLSource   = "URL_SOURCE"
	envURLTarget   = "URL_TARGET"
	envURLHeaders  = "URL_HEADERS"
	envURLTimeout  = "URL_TIMEOUT"
	envURLInsecure = "URL_INSECURE"
	// Auth credentials from Secret (mounted as env vars)
	envURLToken    = "URL_AUTH_TOKEN"
	envURLUsername = "URL_AUTH_USERNAME"
	envURLPassword = "URL_AUTH_PASSWORD"
)

// Default values for url-fetch
const (
	defaultURLTimeout = 30 // seconds
)

func init() {
	rootCmd.AddCommand(urlFetchCmd)
}

var urlFetchCmd = &cobra.Command{
	Use:   "url-fetch",
	Short: "Fetch content from a URL and write to a file",
	Long: `url-fetch fetches content from a remote HTTP/HTTPS URL and writes it to a target file.

This command is used as an init container to fetch URL contexts for Tasks.

Environment variables:
  URL_SOURCE        The URL to fetch content from (required)
  URL_TARGET        Target file path to write content to (required)
  URL_HEADERS       JSON object of HTTP headers to include, e.g., {"X-Custom": "value"}
  URL_TIMEOUT       Request timeout in seconds (default: 30)
  URL_INSECURE      Set to "true" to skip TLS certificate verification
  URL_AUTH_TOKEN    Bearer token for Authorization header (from Secret)
  URL_AUTH_USERNAME Username for HTTP Basic auth (from Secret)
  URL_AUTH_PASSWORD Password for HTTP Basic auth (from Secret)

Authentication priority:
  1. If URL_AUTH_TOKEN is set, uses Bearer token authentication
  2. If URL_AUTH_USERNAME and URL_AUTH_PASSWORD are set, uses HTTP Basic auth
  3. If URL_HEADERS contains Authorization, uses that header
  4. Otherwise, no authentication

Example:
  URL_SOURCE='https://api.example.com/openapi.yaml' \
  URL_TARGET='/workspace/specs/openapi.yaml' \
  URL_TIMEOUT='60' \
  /kubeopencode url-fetch`,
	RunE: runURLFetch,
}

func runURLFetch(cmd *cobra.Command, args []string) error {
	// Get configuration from environment variables
	source := os.Getenv(envURLSource)
	target := os.Getenv(envURLTarget)
	headersJSON := os.Getenv(envURLHeaders)
	timeoutStr := os.Getenv(envURLTimeout)
	insecureStr := os.Getenv(envURLInsecure)

	// Auth credentials (from mounted Secret)
	authToken := os.Getenv(envURLToken)
	authUsername := os.Getenv(envURLUsername)
	authPassword := os.Getenv(envURLPassword)

	// Validate required fields
	if source == "" {
		return fmt.Errorf("URL_SOURCE environment variable is required")
	}
	if target == "" {
		return fmt.Errorf("URL_TARGET environment variable is required")
	}

	fmt.Println("url-fetch: Fetching content from URL...")
	fmt.Printf("  Source: %s\n", source)
	fmt.Printf("  Target: %s\n", target)

	// Parse timeout
	timeout := defaultURLTimeout
	if timeoutStr != "" {
		parsed, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return fmt.Errorf("invalid URL_TIMEOUT value: %w", err)
		}
		timeout = parsed
	}
	fmt.Printf("  Timeout: %ds\n", timeout)

	// Parse insecure flag
	insecure := insecureStr == "true" || insecureStr == "1"
	if insecure {
		fmt.Println("  WARNING: TLS certificate verification disabled")
	}

	// Parse headers
	headers := make(map[string]string)
	if headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
			return fmt.Errorf("failed to parse URL_HEADERS: %w", err)
		}
		fmt.Printf("  Custom headers: %d\n", len(headers))
	}

	// Create HTTP client
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: transport,
	}

	// Create request
	req, err := http.NewRequest("GET", source, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Add authentication (priority: token > basic auth > headers)
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
		fmt.Println("  Auth: Bearer token")
	} else if authUsername != "" && authPassword != "" {
		req.SetBasicAuth(authUsername, authPassword)
		fmt.Println("  Auth: HTTP Basic")
	} else if headers["Authorization"] != "" {
		fmt.Println("  Auth: Custom header")
	} else {
		fmt.Println("  Auth: None")
	}

	// Execute request
	fmt.Println("url-fetch: Executing request...")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("url-fetch: Response status: %s\n", resp.Status)

	// Create target directory if needed
	targetDir := filepath.Dir(target)
	if targetDir != "" && targetDir != "." {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	// Write content to target file
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer file.Close()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	fmt.Printf("url-fetch: Written %d bytes to %s\n", written, target)
	fmt.Println("url-fetch: Done!")
	return nil
}
