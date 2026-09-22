package video

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccess_FileVideoStorageSaveWritesFileAndReturnsPublicPath(t *testing.T) {
	// Arrange
	diskDir := t.TempDir()
	sut := NewFileVideoStorage(diskDir, "/data/videos")
	ctx := t.Context()
	data := []byte("fake video data")

	// Act
	publicPath, err := sut.Save(ctx, "test.mp4", data)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "/data/videos/test.mp4", publicPath)

	diskPath := filepath.Join(diskDir, "test.mp4")
	written, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, data, written)

	info, err := os.Stat(diskPath)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

func TestError_FileVideoStorageSaveFailsWhenDirectoryMissing(t *testing.T) {
	// Arrange
	missingDir := filepath.Join(t.TempDir(), "missing")
	sut := NewFileVideoStorage(missingDir, "/data/videos")

	// Act
	publicPath, err := sut.Save(t.Context(), "test.mp4", []byte("fake video data"))

	// Assert
	assert.Error(t, err)
	assert.Empty(t, publicPath)
}
