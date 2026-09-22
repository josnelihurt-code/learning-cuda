package version

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	unknownValue = "unknown"
)

type repositoryImpl struct {
	goVersionPath    string
	cppVersionPath   string
	protoVersionPath string
}

func findProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := wd
	for {
		goAPIVersionPath := filepath.Join(dir, "src", "go_api", "VERSION")
		if _, err := os.Stat(goAPIVersionPath); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return wd
}

func NewVersionRepository() *repositoryImpl {
	projectRoot := findProjectRoot()
	return &repositoryImpl{
		goVersionPath:    filepath.Join(projectRoot, "src", "go_api", "VERSION"),
		cppVersionPath:   filepath.Join(projectRoot, "src", "cpp_accelerator", "VERSION"),
		protoVersionPath: filepath.Join(projectRoot, "proto", "VERSION"),
	}
}

func (r *repositoryImpl) readVersionFile(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return unknownValue
	}
	return strings.TrimSpace(string(data))
}

func (r *repositoryImpl) GetGoVersion() string {
	return r.readVersionFile(r.goVersionPath)
}

func (r *repositoryImpl) GetProtoVersion() string {
	return r.readVersionFile(r.protoVersionPath)
}
