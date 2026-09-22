package configquery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubStreamSettings struct {
	endpoint string
}

func (s stubStreamSettings) WebRTCSignalingEndpoint() string { return s.endpoint }

type stubToolsCatalog struct {
	env             string
	observability   []Tool
	features        []Tool
	testingTools    []Tool
}

func (s stubToolsCatalog) Environment() string          { return s.env }
func (s stubToolsCatalog) ObservabilityTools() []Tool   { return s.observability }
func (s stubToolsCatalog) FeaturesTools() []Tool        { return s.features }
func (s stubToolsCatalog) TestingTools() []Tool         { return s.testingTools }

func TestGetStreamSettingsUseCase_Execute(t *testing.T) {
	t.Run("Success_ReturnsEndpoint", func(t *testing.T) {
		sut := NewGetStreamSettingsUseCase(stubStreamSettings{endpoint: "wss://example/signal"})
		out, err := sut.Execute(context.Background(), GetStreamSettingsUseCaseInput{})
		require.NoError(t, err)
		assert.Equal(t, "wss://example/signal", out.WebRTCSignalingEndpoint)
	})

	t.Run("Error_EndpointMissing", func(t *testing.T) {
		sut := NewGetStreamSettingsUseCase(stubStreamSettings{})
		_, err := sut.Execute(context.Background(), GetStreamSettingsUseCaseInput{})
		assert.ErrorIs(t, err, ErrSignalingEndpointNotConfigured)
	})
}

func TestGetAvailableToolsUseCase_Execute(t *testing.T) {
	t.Run("Success_CategorizesNonEmptyBuckets", func(t *testing.T) {
		sut := NewGetAvailableToolsUseCase(stubToolsCatalog{
			env:           "dev",
			observability: []Tool{{ID: "jaeger", Name: "Jaeger", Type: "url"}},
			testingTools:  []Tool{{ID: "e2e", Name: "E2E", Type: "action"}},
		})
		out, err := sut.Execute(context.Background(), GetAvailableToolsUseCaseInput{})
		require.NoError(t, err)
		assert.Equal(t, "dev", out.Environment)
		require.Len(t, out.Categories, 2)
		assert.Equal(t, "observability", out.Categories[0].ID)
		assert.Equal(t, "testing", out.Categories[1].ID)
	})

	t.Run("Success_SkipsEmptyBuckets", func(t *testing.T) {
		sut := NewGetAvailableToolsUseCase(stubToolsCatalog{env: "prod"})
		out, err := sut.Execute(context.Background(), GetAvailableToolsUseCaseInput{})
		require.NoError(t, err)
		assert.Empty(t, out.Categories)
	})
}

func TestErrSignalingEndpointNotConfigured(t *testing.T) {
	assert.True(t, errors.Is(ErrSignalingEndpointNotConfigured, ErrSignalingEndpointNotConfigured))
}
