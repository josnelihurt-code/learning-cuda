package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type EvaluateFeatureFlagBooleanUseCaseInput struct {
	FlagKey       string
	EntityID      string
	FallbackValue bool
}

type EvaluateFeatureFlagBooleanUseCaseOutput struct {
	Result bool
}

type EvaluateFeatureFlagBooleanUseCase struct {
	repository featureFlagEvaluator
}

func NewEvaluateFeatureFlagBooleanUseCase(repo featureFlagEvaluator) *EvaluateFeatureFlagBooleanUseCase {
	return &EvaluateFeatureFlagBooleanUseCase{repository: repo}
}

func (uc *EvaluateFeatureFlagBooleanUseCase) Execute(
	ctx context.Context,
	input EvaluateFeatureFlagBooleanUseCaseInput,
) (EvaluateFeatureFlagBooleanUseCaseOutput, error) {
	tracer := otel.Tracer("evaluate-feature-flag")
	ctx, span := tracer.Start(ctx, "EvaluateBoolean",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("flag.key", input.FlagKey),
		attribute.String("flag.entity_id", input.EntityID),
		attribute.Bool("flag.fallback_value", input.FallbackValue),
	)

	eval, err := uc.repository.EvaluateBoolean(ctx, input.FlagKey, input.EntityID)
	if err != nil || !eval.Success {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Bool("fallback_value", input.FallbackValue).
			Err(err).
			Msg("Feature flag evaluation failed, using fallback")
		span.SetAttributes(attribute.Bool("flag.used_fallback", true))
		span.SetAttributes(attribute.Bool("flag.fallback_value", input.FallbackValue))
		return EvaluateFeatureFlagBooleanUseCaseOutput{Result: input.FallbackValue}, nil
	}

	result, ok := eval.Result.(bool)
	if !ok {
		log.FromContext(ctx).Warn().
			Str("flag_key", input.FlagKey).
			Msg("Type assertion failed for flag result, using fallback value")
		span.SetAttributes(attribute.Bool("flag.used_fallback", true))
		span.SetAttributes(attribute.Bool("flag.fallback_value", input.FallbackValue))
		return EvaluateFeatureFlagBooleanUseCaseOutput{Result: input.FallbackValue}, nil
	}
	span.SetAttributes(attribute.Bool("flag.used_fallback", false))
	span.SetAttributes(attribute.Bool("flag.fallback_value", input.FallbackValue))

	log.FromContext(ctx).Debug().Str("flag_key", input.FlagKey).Bool("result", result).Msg("Feature flag evaluated")
	return EvaluateFeatureFlagBooleanUseCaseOutput{Result: result}, nil
}
