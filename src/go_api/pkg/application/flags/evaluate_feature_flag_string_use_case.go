package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	tracer := otel.Tracer("evaluate-feature-flag")
	ctx, span := tracer.Start(ctx, "EvaluateString",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("flag.key", input.FlagKey),
		attribute.String("flag.entity_id", input.EntityID),
		attribute.String("flag.fallback_value", input.FallbackValue),
	)

	eval, err := uc.repository.EvaluateString(ctx, input.FlagKey, input.EntityID)
	if err != nil || !eval.Success {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Str("fallback_value", input.FallbackValue).
			Err(err).
			Msg("Feature flag evaluation failed, using fallback")
		span.SetAttributes(attribute.Bool("flag.used_fallback", true))
		span.SetAttributes(attribute.String("flag.fallback_value", input.FallbackValue))
		return EvaluateFeatureFlagStringUseCaseOutput{Result: input.FallbackValue}, nil
	}

	result, ok := eval.Result.(string)
	if !ok {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Msg("Type assertion failed for flag result, using fallback value")
		span.SetAttributes(attribute.Bool("flag.used_fallback", true))
		span.SetAttributes(attribute.String("flag.fallback_value", input.FallbackValue))
		return EvaluateFeatureFlagStringUseCaseOutput{Result: input.FallbackValue}, nil
	}
	span.SetAttributes(attribute.Bool("flag.used_fallback", false))
	span.SetAttributes(attribute.String("flag.fallback_value", result))

	log.FromContext(ctx).Debug().Str("flag_key", input.FlagKey).Str("result", result).Msg("Feature flag evaluated")
	return EvaluateFeatureFlagStringUseCaseOutput{Result: result}, nil
}
