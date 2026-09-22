package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
)

type EvaluateFeatureFlagStringUseCaseInput struct {
	FlagKey       string
	EntityID      string
	FallbackValue string
}

type EvaluateFeatureFlagStringUseCaseOutput struct {
	Result string
}

type evaluateFeatureFlagStringUseCase struct {
	repository featureFlagEvaluator
}

func NewEvaluateFeatureFlagStringUseCase(repo featureFlagEvaluator) *evaluateFeatureFlagStringUseCase {
	return &evaluateFeatureFlagStringUseCase{repository: repo}
}

func (uc *evaluateFeatureFlagStringUseCase) Execute(
	ctx context.Context,
	input EvaluateFeatureFlagStringUseCaseInput,
) (EvaluateFeatureFlagStringUseCaseOutput, error) {
	eval, err := uc.repository.EvaluateString(ctx, input.FlagKey, input.EntityID)
	if err != nil || !eval.Success {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Str("fallback_value", input.FallbackValue).
			Err(err).
			Msg("Feature flag evaluation failed, using fallback")
		return EvaluateFeatureFlagStringUseCaseOutput{Result: input.FallbackValue}, nil
	}

	result, ok := eval.Result.(string)
	if !ok {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Msg("Type assertion failed for flag result, using fallback value")
		return EvaluateFeatureFlagStringUseCaseOutput{Result: input.FallbackValue}, nil
	}

	log.FromContext(ctx).Debug().
		Str("flag_key", input.FlagKey).
		Str("result", result).
		Msg("Feature flag evaluated")
	return EvaluateFeatureFlagStringUseCaseOutput{Result: result}, nil
}
