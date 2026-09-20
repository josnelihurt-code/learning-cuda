package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservabilityConfig_AuthHeader(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ObservabilityConfig
		expected string
	}{
		{
			name:     "NoToken_ReturnsEmpty",
			cfg:      ObservabilityConfig{OtelAuthInstanceID: "123"},
			expected: "",
		},
		{
			name: "TokenAndInstanceID_ReturnsBasicAuth",
			cfg: ObservabilityConfig{
				OtelAuthInstanceID: "1836703",
				OtelAuthToken:      "glc_test-token",
			},
			expected: "Basic " + base64.StdEncoding.EncodeToString([]byte("1836703:glc_test-token")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.AuthHeader())
		})
	}
}

func TestObservabilityConfig_UsesHTTPProtocol(t *testing.T) {
	assert.True(t, (&ObservabilityConfig{OtelExporterProtocol: OtelProtocolHTTP}).UsesHTTPProtocol())
	assert.False(t, (&ObservabilityConfig{OtelExporterProtocol: OtelProtocolGRPC}).UsesHTTPProtocol())
	assert.False(t, (&ObservabilityConfig{}).UsesHTTPProtocol())
}

func TestConfig_OtelAuthTokenFromEnv(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
observability:
  enabled: true
  otel_auth_instance_id: "1836703"
`), 0o600))

	t.Run("EnvTokenIsBoundIntoConfig", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TOKEN", "glc_from-env")
		cfg := New(configFile)
		assert.Equal(t, "glc_from-env", cfg.Observability.OtelAuthToken)
		assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("1836703:glc_from-env")), cfg.Observability.AuthHeader())
	})

	t.Run("NoEnvToken_LeavesAuthEmpty", func(t *testing.T) {
		cfg := New(configFile)
		assert.Empty(t, cfg.Observability.AuthHeader())
	})
}

func TestManager_Redacted(t *testing.T) {
	manager := &Manager{
		Observability: ObservabilityConfig{
			ServiceName:   "cuda-image-processor",
			OtelAuthToken: "glc_secret",
		},
	}

	redacted := manager.Redacted()

	assert.Equal(t, "[redacted]", redacted.Observability.OtelAuthToken)
	assert.Equal(t, "cuda-image-processor", redacted.Observability.ServiceName)
	assert.Equal(t, "glc_secret", manager.Observability.OtelAuthToken, "original must stay untouched")
}
