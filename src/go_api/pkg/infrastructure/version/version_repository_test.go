package version

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVersionRepository(t *testing.T) {
	repo := NewVersionRepository()
	require.NotNil(t, repo)
	assert.NotEmpty(t, repo.goVersionPath)
	assert.NotEmpty(t, repo.cppVersionPath)
	assert.NotEmpty(t, repo.protoVersionPath)
}

func TestRepositoryImpl_GetGoVersion(t *testing.T) {
	repo := NewVersionRepository()

	version := repo.GetGoVersion()

	if _, err := os.Stat(filepath.Join("..", "..", "..", "..", "..", "src", "go_api", "VERSION")); err == nil {
		assert.NotEqual(t, UnknownValue, version)
		assert.NotEmpty(t, version)
	} else {
		assert.Equal(t, UnknownValue, version)
	}
}

// GetCppVersion is not implemented on RepositoryImpl (tracked as backlog).
// This test covers the real API such an implementation would rely on: the
// constructor-resolved cpp VERSION path and the readVersionFile fallback.
func TestRepositoryImpl_CppVersionFile(t *testing.T) {
	repo := NewVersionRepository()
	require.NotEmpty(t, repo.cppVersionPath)

	version := repo.readVersionFile(repo.cppVersionPath)

	if _, err := os.Stat(filepath.Join("..", "..", "..", "..", "..", "src", "cpp_accelerator", "VERSION")); err == nil {
		assert.NotEqual(t, UnknownValue, version)
		assert.NotEmpty(t, version)
	} else {
		assert.Equal(t, UnknownValue, version)
	}
}

func TestRepositoryImpl_GetProtoVersion(t *testing.T) {
	repo := NewVersionRepository()

	version := repo.GetProtoVersion()

	if _, err := os.Stat(filepath.Join("..", "..", "..", "..", "..", "proto", "VERSION")); err == nil {
		assert.NotEqual(t, UnknownValue, version)
		assert.NotEmpty(t, version)
	} else {
		assert.Equal(t, UnknownValue, version)
	}
}

func TestRepositoryImpl_readVersionFile_NonExistent(t *testing.T) {
	repo := &RepositoryImpl{
		goVersionPath: "/nonexistent/path/VERSION",
	}

	version := repo.readVersionFile("/nonexistent/path/VERSION")
	assert.Equal(t, UnknownValue, version)
}

func TestFindProjectRoot(t *testing.T) {
	root := findProjectRoot()
	assert.NotEmpty(t, root)

	goAPIVersionPath := filepath.Join(root, "src", "go_api", "VERSION")
	_, err := os.Stat(goAPIVersionPath)
	assert.NoError(t, err, "src/go_api/VERSION should exist in project root")
}
