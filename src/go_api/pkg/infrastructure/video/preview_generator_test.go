package video

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePreview(t *testing.T) {
	ctx := t.Context()

	t.Run("generate preview for valid video", func(t *testing.T) {
		// TODO: Use golden test data (add small test video <1MB to data/test-data/videos/ directory)
		videoPath := "/data/videos/sample.mp4"
		if _, err := os.Stat(videoPath); errors.Is(err, fs.ErrNotExist) {
			t.Skip("Sample video not found, skipping test")
		}

		tempDir := t.TempDir()
		previewPath := filepath.Join(tempDir, "test-preview.png")

		err := GeneratePreview(ctx, videoPath, previewPath)
		require.NoError(t, err)

		stat, err := os.Stat(previewPath)
		require.NoError(t, err)
		assert.Greater(t, stat.Size(), int64(0), "Preview file should not be empty")
	})

	t.Run("fail for invalid video path", func(t *testing.T) {
		tempDir := t.TempDir()
		previewPath := filepath.Join(tempDir, "invalid-preview.png")

		err := GeneratePreview(ctx, "/nonexistent/video.mp4", previewPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ffprobe failed")
	})

	t.Run("fail for corrupted video", func(t *testing.T) {
		tempDir := t.TempDir()
		corruptedVideoPath := filepath.Join(tempDir, "corrupted.mp4")
		previewPath := filepath.Join(tempDir, "corrupted-preview.png")

		err := os.WriteFile(corruptedVideoPath, []byte("not a video"), 0o600)
		require.NoError(t, err)

		err = GeneratePreview(ctx, corruptedVideoPath, previewPath)
		assert.Error(t, err)
	})
}
