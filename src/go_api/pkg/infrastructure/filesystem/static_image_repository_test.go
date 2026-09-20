package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStaticImageRepository(t *testing.T) {
	directory := "/data/static_images"
	repo := NewStaticImageRepository(directory)

	assert.NotNil(t, repo)
	assert.Equal(t, directory, repo.directory)
}

func TestStaticImageRepository_FindAll(t *testing.T) {
	tests := []struct {
		name     string
		setupDir func(t *testing.T) string
		validate func(t *testing.T, images []any, err error)
	}{
		{
			name: "returns empty list when directory does not exist",
			setupDir: func(t *testing.T) string {
				return "/nonexistent/directory"
			},
			validate: func(t *testing.T, images []any, err error) {
				assert.NoError(t, err)
				assert.Empty(t, images)
			},
		},
		{
			name: "returns empty list when directory is empty",
			setupDir: func(t *testing.T) string {
				return t.TempDir()
			},
			validate: func(t *testing.T, images []any, err error) {
				assert.NoError(t, err)
				assert.Empty(t, images)
			},
		},
		{
			name: "scans and returns PNG files",
			setupDir: func(t *testing.T) string {
				tmpDir := t.TempDir()
				createFile(t, tmpDir, "lena.png")
				createFile(t, tmpDir, "mandrill.png")
				createFile(t, tmpDir, "peppers.png")
				return tmpDir
			},
			validate: func(t *testing.T, images []any, err error) {
				assert.NoError(t, err)
				require.Len(t, images, 3)
			},
		},
		{
			name: "ignores non-image files",
			setupDir: func(t *testing.T) string {
				tmpDir := t.TempDir()
				createFile(t, tmpDir, "lena.png")
				createFile(t, tmpDir, "readme.txt")
				createFile(t, tmpDir, "data.json")
				return tmpDir
			},
			validate: func(t *testing.T, images []any, err error) {
				assert.NoError(t, err)
				require.Len(t, images, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := tt.setupDir(t)
			repo := NewStaticImageRepository(directory)
			ctx := t.Context()

			result, err := repo.FindAll(ctx)

			var images []any
			for _, img := range result {
				images = append(images, img)
			}

			tt.validate(t, images, err)
		})
	}
}

func createFile(t *testing.T, dir, filename string) {
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte("dummy"), 0o644)
	require.NoError(t, err)
}

func TestStaticImageRepository_Save(t *testing.T) {
	pngHeader := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13}

	tests := []struct {
		name         string
		filename     string
		fileData     []byte
		assertResult func(t *testing.T, image any, err error, dir string)
	}{
		{
			name:     "Success_SavesPNGFile",
			filename: "test-upload.png",
			fileData: pngHeader,
			assertResult: func(t *testing.T, image any, err error, dir string) {
				assert.NoError(t, err)
				require.NotNil(t, image)

				savedPath := filepath.Join(dir, "test-upload.png")
				_, statErr := os.Stat(savedPath)
				assert.NoError(t, statErr)

				content, readErr := os.ReadFile(savedPath)
				assert.NoError(t, readErr)
				assert.Equal(t, pngHeader, content)
			},
		},
		{
			name:     "Success_CreatesCorrectMetadata",
			filename: "my-image.png",
			fileData: pngHeader,
			assertResult: func(t *testing.T, image any, err error, dir string) {
				assert.NoError(t, err)
				require.NotNil(t, image)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			tmpDir := t.TempDir()
			repo := NewStaticImageRepository(tmpDir)
			ctx := t.Context()

			// Act
			result, err := repo.Save(ctx, tt.filename, tt.fileData)

			// Assert
			tt.assertResult(t, result, err, tmpDir)
		})
	}
}
