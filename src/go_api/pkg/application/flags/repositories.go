package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

// featureFlagEvaluator is the read-only feature-flag port for the evaluation use cases.
type featureFlagEvaluator interface {
	EvaluateBoolean(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error)
	EvaluateString(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error)
}

// featureFlagAdmin is the feature-flag management port for list/upsert use cases.
type featureFlagAdmin interface {
	ListFlags(ctx context.Context) ([]domain.FeatureFlag, error)
	UpsertFlag(ctx context.Context, flag domain.FeatureFlag) error
}
