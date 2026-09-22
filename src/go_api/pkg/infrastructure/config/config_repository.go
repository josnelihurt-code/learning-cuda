package config

import (
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
)

type repositoryImpl struct {
	configManager *config.Manager
}

func NewConfigRepository(configManager *config.Manager) *repositoryImpl {
	return &repositoryImpl{
		configManager: configManager,
	}
}

func (r *repositoryImpl) GetEnvironment() string {
	if r.configManager == nil {
		return "production"
	}
	return r.configManager.Environment
}
