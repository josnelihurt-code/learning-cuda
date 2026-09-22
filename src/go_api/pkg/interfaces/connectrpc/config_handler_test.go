package connectrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfigHandler(t *testing.T) {
	// Arrange / Act
	sut := NewConfigHandler(ConfigHandlerDeps{})

	// Assert
	require.NotNil(t, sut)
}
