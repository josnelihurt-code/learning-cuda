package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
)

type EvaluateFeatureFlagBooleanUseCaseInput struct {
	FlagKey       string
	EntityID      string
	FallbackValue bool
}

type EvaluateFeatureFlagBooleanUseCaseOutput struct {
	Result bool
}

type evaluateFeatureFlagBooleanUseCase struct {
	repository featureFlagEvaluator
}

func NewEvaluateFeatureFlagBooleanUseCase(repo featureFlagEvaluator) *evaluateFeatureFlagBooleanUseCase {
	return &evaluateFeatureFlagBooleanUseCase{repository: repo}
}

func (uc *evaluateFeatureFlagBooleanUseCase) Execute(
	ctx context.Context,
	input EvaluateFeatureFlagBooleanUseCaseInput,
) (EvaluateFeatureFlagBooleanUseCaseOutput, error) {
	eval, err := uc.repository.EvaluateBoolean(ctx, input.FlagKey, input.EntityID)
	if err != nil || !eval.Success {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Bool("fallback_value", input.FallbackValue).
			Err(err).
			Msg("Feature flag evaluation failed, using fallback")
		return EvaluateFeatureFlagBooleanUseCaseOutput{Result: input.FallbackValue}, nil
	}

	result, ok := eval.Result.(bool)
	if !ok {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Msg("Type assertion failed for flag result, using fallback value")
		return EvaluateFeatureFlagBooleanUseCaseOutput{Result: input.FallbackValue}, nil
	}

	log.FromContext(ctx).Debug().
		Str("flag_key", input.FlagKey).
		Bool("result", result).
		Msg("Feature flag evaluated")
	return EvaluateFeatureFlagBooleanUseCaseOutput{Result: result}, nil
}
