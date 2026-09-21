package video

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError_FilePreviewGeneratorRepositoryGenerateFailsForMissingVideo(t *testing.T) {
	// Arrange
	sut := NewFilePreviewGeneratorRepository(t.TempDir(), "/data/video_previews")

	// Act
	previewPath, err := sut.Generate(t.Context(), "missing", "/data/videos/missing.mp4")

	// Assert
	assert.Error(t, err)
	assert.Empty(t, previewPath)
}
