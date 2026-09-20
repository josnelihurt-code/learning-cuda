package featureflags

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"gopkg.in/yaml.v3"
)

type goffFlagConfig struct {
	Variations  map[string]any `yaml:"variations"`
	DefaultRule struct {
		Variation string `yaml:"variation"`
	} `yaml:"defaultRule"`
}

type GoffRepository struct {
	filePath string
	mu       sync.RWMutex
}

func NewGoffRepository(filePath string) *GoffRepository {
	return &GoffRepository{filePath: filePath}
}

func (r *GoffRepository) ValidateConfig() error {
	_, err := r.readConfig()
	return err
}

func (r *GoffRepository) EvaluateBoolean(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error) {
	_ = ctx
	_ = entityID
	flag, err := r.GetFlag(context.Background(), flagKey)
	if err != nil {
		return &domain.FeatureFlagEvaluation{
			FlagKey:      flagKey,
			EntityID:     entityID,
			Result:       false,
			Success:      false,
			UsedFallback: true,
		}, err
	}
	value, ok := flag.DefaultValue.(bool)
	if !ok {
		return &domain.FeatureFlagEvaluation{
			FlagKey:      flagKey,
			EntityID:     entityID,
			Result:       false,
			Success:      false,
			UsedFallback: true,
		}, fmt.Errorf("flag %s is not boolean", flagKey)
	}
	return &domain.FeatureFlagEvaluation{
		FlagKey:      flagKey,
		EntityID:     entityID,
		Result:       value,
		Success:      true,
		UsedFallback: false,
	}, nil
}

func (r *GoffRepository) EvaluateString(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error) {
	_ = ctx
	_ = entityID
	flag, err := r.GetFlag(context.Background(), flagKey)
	if err != nil {
		return &domain.FeatureFlagEvaluation{
			FlagKey:      flagKey,
			EntityID:     entityID,
			Result:       "",
			Success:      false,
			UsedFallback: true,
		}, err
	}
	value, ok := flag.DefaultValue.(string)
	if !ok {
		return &domain.FeatureFlagEvaluation{
			FlagKey:      flagKey,
			EntityID:     entityID,
			Result:       "",
			Success:      false,
			UsedFallback: true,
		}, fmt.Errorf("flag %s is not variant", flagKey)
	}
	return &domain.FeatureFlagEvaluation{
		FlagKey:      flagKey,
		EntityID:     entityID,
		Result:       value,
		Success:      true,
		UsedFallback: false,
	}, nil
}

func (r *GoffRepository) GetFlag(ctx context.Context, flagKey string) (*domain.FeatureFlag, error) {
	_ = ctx
	flags, err := r.readAll()
	if err != nil {
		return nil, err
	}
	flag, ok := flags[flagKey]
	if !ok {
		return nil, fmt.Errorf("flag %s not found", flagKey)
	}
	return &flag, nil
}

func (r *GoffRepository) ListFlags(ctx context.Context) ([]domain.FeatureFlag, error) {
	_ = ctx
	flags, err := r.readAll()
	if err != nil {
		return nil, err
	}
	result := make([]domain.FeatureFlag, 0, len(flags))
	for _, flag := range flags {
		result = append(result, flag)
	}
	return result, nil
}

func (r *GoffRepository) UpsertFlag(ctx context.Context, flag domain.FeatureFlag) error {
	_ = ctx
	flags, err := r.readAll()
	if err != nil {
		return err
	}
	flags[flag.Key] = flag
	return r.writeAll(flags)
}

func (r *GoffRepository) readAll() (map[string]domain.FeatureFlag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	configs, err := r.readConfig()
	if err != nil {
		return nil, err
	}
	result := make(map[string]domain.FeatureFlag, len(configs))
	for key, cfg := range configs {
		defaultValue, flagType := extractDefault(cfg)
		result[key] = domain.FeatureFlag{
			Key:          key,
			Name:         key,
			Type:         flagType,
			Enabled:      true,
			DefaultValue: defaultValue,
		}
	}
	return result, nil
}

func (r *GoffRepository) writeAll(flags map[string]domain.FeatureFlag) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	configs := make(map[string]goffFlagConfig, len(flags))
	for _, flag := range flags {
		cfg := goffFlagConfig{
			Variations: make(map[string]any),
		}
		switch flag.Type {
		case domain.BooleanFlagType:
			cfg.Variations["enabled"] = true
			cfg.Variations["disabled"] = false
			if v, ok := flag.DefaultValue.(bool); ok && v {
				cfg.DefaultRule.Variation = "enabled"
			} else {
				cfg.DefaultRule.Variation = "disabled"
			}
		default:
			defaultValue := fmt.Sprintf("%v", flag.DefaultValue)
			cfg.Variations[defaultValue] = defaultValue
			cfg.DefaultRule.Variation = defaultValue
		}
		configs[flag.Key] = cfg
	}
	return r.writeConfig(configs)
}

func (r *GoffRepository) readConfig() (map[string]goffFlagConfig, error) {
	if _, err := os.Stat(r.filePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("goff file not found: %s", r.filePath)
		}
		return nil, fmt.Errorf("stat goff file: %w", err)
	}
	raw, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("read goff file: %w", err)
	}
	var config map[string]goffFlagConfig
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("unmarshal goff file: %w", err)
	}
	return config, nil
}

func (r *GoffRepository) writeConfig(config map[string]goffFlagConfig) error {
	if err := os.MkdirAll(filepath.Dir(r.filePath), 0o755); err != nil {
		return fmt.Errorf("create goff directory: %w", err)
	}
	raw, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal goff file: %w", err)
	}
	if err := os.WriteFile(r.filePath, raw, 0o644); err != nil {
		return fmt.Errorf("write goff file: %w", err)
	}
	return nil
}

func extractDefault(cfg goffFlagConfig) (any, domain.FeatureFlagType) {
	if value, ok := cfg.Variations[cfg.DefaultRule.Variation]; ok {
		switch typed := value.(type) {
		case bool:
			return typed, domain.BooleanFlagType
		case string:
			return typed, domain.StringFlagType
		}
	}
	return "", domain.StringFlagType
}
