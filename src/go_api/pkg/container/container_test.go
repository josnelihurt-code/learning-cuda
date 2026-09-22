package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
)

func TestError_NewFailsFastWhenGoFeatureFlagDisabled(t *testing.T) {
	// Arrange
	configFilePath := filepath.Join(t.TempDir(), "config.yaml")
	contents := "go_feature_flag:\n  enabled: false\n"
	require.NoError(t, os.WriteFile(configFilePath, []byte(contents), 0o644))

	// Act
	sut, err := New(t.Context(), configFilePath)

	// Assert
	require.Error(t, err)
	assert.Nil(t, sut)
	assert.Contains(t, err.Error(), "go_feature_flag.enabled is false")
}

func TestValidateFeatureFlagConfig(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		expectError bool
	}{
		{
			name:        "Success_Enabled",
			enabled:     true,
			expectError: false,
		},
		{
			name:        "Error_Disabled",
			enabled:     false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cfg := &config.Manager{}
			cfg.GoFeatureFlag.Enabled = tt.enabled

			// Act
			err := validateFeatureFlagConfig(cfg)

			// Assert
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "go_feature_flag.enabled is false")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
