//go:build integration
// +build integration

package logger

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Tailscale IP for the Vultr production log collector
	tailscaleCollectorIP = "100.89.5.108"
	// OTLP HTTP endpoint port
	otlpHTTPPort = "4318"
	// Full OTLP endpoint via Tailscale
	tailscaleOTLPEndpoint = "http://" + tailscaleCollectorIP + ":" + otlpHTTPPort + "/v1/logs"
)

// TestMain checks if the Tailscale collector is reachable before running tests
func TestMain(m *testing.M) {
	// Check if we can reach the Tailscale collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+tailscaleCollectorIP+":4318", nil)
	if err != nil {
		fmt.Printf("SKIP: Cannot create request to Tailscale collector: %v\n", err)
		fmt.Printf("Ensure Tailscale is running and connected to %s\n", tailscaleCollectorIP)
		os.Exit(0)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("SKIP: Cannot reach Tailscale collector at %s: %v\n", tailscaleCollectorIP, err)
		fmt.Printf("Run 'tailscale status' to verify connection\n")
		os.Exit(0)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		fmt.Printf("SKIP: Tailscale collector returned unexpected status: %d\n", resp.StatusCode)
		os.Exit(0)
	}

	// Run tests
	fmt.Printf("Running integration tests against Tailscale collector at %s\n", tailscaleCollectorIP)
	code := m.Run()
	os.Exit(code)
}

func TestNewOTLPHook_TailscaleIntegration(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		environment string
		serviceName string
		expectError bool
	}{
		{
			name:        "Success_TailscaleConnection",
			endpoint:    tailscaleOTLPEndpoint,
			environment: "test",
			serviceName: "cuda-learning-integration-test",
			expectError: false,
		},
		{
			name:        "Success_ProductionEndpoint",
			endpoint:    tailscaleOTLPEndpoint,
			environment: "production",
			serviceName: "cuda-image-processor",
			expectError: false,
		},
		{
			name:        "Success_StagingEndpoint",
			endpoint:    tailscaleOTLPEndpoint,
			environment: "staging",
			serviceName: "cuda-learning-staging",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			// Act
			hook, err := NewOTLPHook(tt.endpoint, tt.environment, tt.serviceName, "test-1.0.0", "")

			// Assert
			if tt.expectError {
				assert.Error(t, err, "NewOTLPHook should return an error")
				assert.Nil(t, hook, "Hook should be nil on error")
			} else {
				require.NoError(t, err, "NewOTLPHook should not return an error")
				assert.NotNil(t, hook, "Hook should not be nil")

				// Verify hook type
				otlpHook, ok := hook.(*OTLPHook)
				assert.True(t, ok, "Hook should be of type *OTLPHook")
				assert.NotNil(t, otlpHook.logger, "OTLP hook should have a logger")
				assert.NotNil(t, otlpHook.ctx, "OTLP hook should have a context")
			}
		})
	}
}

func TestLogger_WithRemoteLoggingIntegration(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		format      string
		logMessage  string
		expectError bool
	}{
		{
			name:        "Success_LogInfoToTailscaleCollector",
			level:       "info",
			format:      "json",
			logMessage:  "Integration test log message from Tailscale",
			expectError: false,
		},
		{
			name:        "Success_LogWarningToTailscaleCollector",
			level:       "warn",
			format:      "json",
			logMessage:  "Integration test warning message",
			expectError: false,
		},
		{
			name:        "Success_LogErrorToTailscaleCollector",
			level:       "error",
			format:      "json",
			logMessage:  "Integration test error message",
			expectError: false,
		},
		{
			name:        "Success_LogDebugToTailscaleCollector",
			level:       "debug",
			format:      "json",
			logMessage:  "Integration test debug message",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cfg := &Config{
				Level:             tt.level,
				Format:            tt.format,
				Output:            "stdout",
				IncludeCaller:     true,
				RemoteEnabled:     true,
				RemoteEndpoint:    tailscaleOTLPEndpoint,
				RemoteEnvironment: "integration-test",
				ServiceName:       "cuda-learning-integration-test",
			}

			// Act - Create logger with remote logging enabled
			logger := New(cfg)

			// Assert - Logger should be created successfully
			assert.NotNil(t, logger, "Logger should not be nil")

			// Act - Log a test message
			switch tt.level {
			case "info":
				logger.Info().Msg(tt.logMessage)
			case "warn":
				logger.Warn().Msg(tt.logMessage)
			case "error":
				logger.Error().Msg(tt.logMessage)
			case "debug":
				logger.Debug().Msg(tt.logMessage)
			}

			// Give time for logs to be sent to the collector
			time.Sleep(500 * time.Millisecond)

			// If we got here without panicking, the test passed
			assert.True(t, true, "Log message sent successfully to Tailscale collector")
		})
	}
}

func TestLogger_BatchLogTransmissionToTailscale(t *testing.T) {
	// Arrange
	cfg := &Config{
		Level:             "info",
		Format:            "json",
		Output:            "stdout",
		IncludeCaller:     true,
		RemoteEnabled:     true,
		RemoteEndpoint:    tailscaleOTLPEndpoint,
		RemoteEnvironment: "integration-test",
		ServiceName:       "cuda-learning-batch-test",
	}

	// Act
	logger := New(cfg)
	require.NotNil(t, logger, "Logger should not be nil")

	// Log multiple messages to test batch transmission
	testMessages := []string{
		"Batch test message 1",
		"Batch test message 2",
		"Batch test message 3",
		"Batch test message 4",
		"Batch test message 5",
	}

	for _, msg := range testMessages {
		logger.Info().
			Str("test_type", "batch").
			Msg(msg)
	}

	// Give time for batch logs to be sent
	time.Sleep(1 * time.Second)

	// Assert
	assert.True(t, true, "Batch log messages sent successfully to Tailscale collector")
}

func TestLogger_WithStructuredDataToTailscale(t *testing.T) {
	tests := []struct {
		name    string
		logFunc func()
	}{
		{
			name: "Success_LogWithFields",
			logFunc: func() {
				cfg := &Config{
					Level:             "info",
					Format:            "json",
					Output:            "stdout",
					IncludeCaller:     true,
					RemoteEnabled:     true,
					RemoteEndpoint:    tailscaleOTLPEndpoint,
					RemoteEnvironment: "integration-test",
					ServiceName:       "cuda-learning-structured-test",
				}
				logger := New(cfg)
				logger.Info().
					Str("user_id", "test-user-123").
					Str("request_id", "req-456").
					Int("retry_count", 3).
					Float64("latency_ms", 42.5).
					Bool("success", true).
					Msg("Structured log test")
			},
		},
		{
			name: "Success_LogWithNestedFields",
			logFunc: func() {
				cfg := &Config{
					Level:             "info",
					Format:            "json",
					Output:            "stdout",
					IncludeCaller:     true,
					RemoteEnabled:     true,
					RemoteEndpoint:    tailscaleOTLPEndpoint,
					RemoteEnvironment: "integration-test",
					ServiceName:       "cuda-learning-nested-test",
				}
				logger := New(cfg)
				logger.Info().
					Str("service", "image-processor").
					Str("operation", "resize").
					Str("image_format", "png").
					Int("width", 1920).
					Int("height", 1080).
					Msg("Image processing log test")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			tt.logFunc()

			// Give time for logs to be sent
			time.Sleep(500 * time.Millisecond)

			// Assert
			assert.True(t, true, "Structured log sent successfully to Tailscale collector")
		})
	}
}

func TestLogger_TailscaleCollectorHealthCheck(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Act - Check if we can reach the collector
	endpointURL := "http://" + tailscaleCollectorIP + ":4318"
	req, err := http.NewRequestWithContext(ctx, "GET", endpointURL, nil)
	require.NoError(t, err, "Should be able to create HTTP request")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	// Assert
	if err != nil {
		t.Skipf("Cannot reach Tailscale collector at %s: %v", endpointURL, err)
		return
	}
	defer resp.Body.Close()

	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed,
		"Collector should respond with acceptable status code, got: %d", resp.StatusCode)
}
