package flags

import (
	"context"
	"errors"
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockFeatureFlagRepository struct {
	mock.Mock
}

func (m *mockFeatureFlagRepository) EvaluateBoolean(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error) {
	args := m.Called(ctx, flagKey, entityID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.FeatureFlagEvaluation), args.Error(1)
}

func (m *mockFeatureFlagRepository) EvaluateString(ctx context.Context, flagKey, entityID string) (*domain.FeatureFlagEvaluation, error) {
	args := m.Called(ctx, flagKey, entityID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.FeatureFlagEvaluation), args.Error(1)
}

func (m *mockFeatureFlagRepository) SyncFlags(ctx context.Context, flags []domain.FeatureFlag) error {
	args := m.Called(ctx, flags)
	return args.Error(0)
}

func (m *mockFeatureFlagRepository) GetFlag(ctx context.Context, flagKey string) (*domain.FeatureFlag, error) {
	args := m.Called(ctx, flagKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.FeatureFlag), args.Error(1)
}

func (m *mockFeatureFlagRepository) ListFlags(ctx context.Context) ([]domain.FeatureFlag, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.FeatureFlag), args.Error(1)
}

func (m *mockFeatureFlagRepository) UpsertFlag(ctx context.Context, flag domain.FeatureFlag) error {
	args := m.Called(ctx, flag)
	return args.Error(0)
}

func makeBooleanEvaluation(result, success bool) *domain.FeatureFlagEvaluation {
	return &domain.FeatureFlagEvaluation{
		FlagKey:      "test_flag",
		EntityID:     "test_entity",
		Result:       result,
		Success:      success,
		UsedFallback: !success,
	}
}

func makeStringEvaluation(result string, success bool) *domain.FeatureFlagEvaluation {
	return &domain.FeatureFlagEvaluation{
		FlagKey:      "test_flag",
		EntityID:     "test_entity",
		Result:       result,
		Success:      success,
		UsedFallback: !success,
	}
}

func TestNewEvaluateFeatureFlagBooleanUseCase(t *testing.T) {
	// Arrange
	repo := new(mockFeatureFlagRepository)

	// Act
	sut := NewEvaluateFeatureFlagBooleanUseCase(repo)

	// Assert
	require.NotNil(t, sut)
	assert.Equal(t, repo, sut.repository)
}

func TestEvaluateFeatureFlagBooleanUseCase_Execute(t *testing.T) {
	var (
		errRepositoryFailed = errors.New("repository failed")
	)

	tests := []struct {
		name         string
		input        EvaluateFeatureFlagBooleanUseCaseInput
		mockResult   *domain.FeatureFlagEvaluation
		mockError    error
		assertResult func(t *testing.T, result bool, err error)
	}{
		{
			name: "Success_BooleanTrue",
			input: EvaluateFeatureFlagBooleanUseCaseInput{
				FlagKey:       "feature_enabled",
				EntityID:      "user_123",
				FallbackValue: false,
			},
			mockResult: makeBooleanEvaluation(true, true),
			mockError:  nil,
			assertResult: func(t *testing.T, result bool, err error) {
				assert.NoError(t, err)
				assert.True(t, result)
			},
		},
		{
			name: "Success_BooleanFalse",
			input: EvaluateFeatureFlagBooleanUseCaseInput{
				FlagKey:       "feature_disabled",
				EntityID:      "user_456",
				FallbackValue: true,
			},
			mockResult: makeBooleanEvaluation(false, true),
			mockError:  nil,
			assertResult: func(t *testing.T, result bool, err error) {
				assert.NoError(t, err)
				assert.False(t, result)
			},
		},
		{
			name: "Error_BooleanWithFallback",
			input: EvaluateFeatureFlagBooleanUseCaseInput{
				FlagKey:       "broken_flag",
				EntityID:      "user_789",
				FallbackValue: true,
			},
			mockResult: nil,
			mockError:  errRepositoryFailed,
			assertResult: func(t *testing.T, result bool, err error) {
				assert.NoError(t, err)
				assert.True(t, result)
			},
		},
		{
			name: "Edge_BooleanEvaluationFailedUseFallback",
			input: EvaluateFeatureFlagBooleanUseCaseInput{
				FlagKey:       "failed_flag",
				EntityID:      "user_000",
				FallbackValue: false,
			},
			mockResult: makeBooleanEvaluation(true, false),
			mockError:  nil,
			assertResult: func(t *testing.T, result bool, err error) {
				assert.NoError(t, err)
				assert.False(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := new(mockFeatureFlagRepository)
			mockRepo.On("EvaluateBoolean",
				mock.Anything,
				tt.input.FlagKey,
				tt.input.EntityID,
			).Return(tt.mockResult, tt.mockError).Once()

			sut := NewEvaluateFeatureFlagBooleanUseCase(mockRepo)
			ctx := t.Context()

			// Act
			result, err := sut.Execute(ctx, tt.input)

			// Assert
			tt.assertResult(t, result.Result, err)
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestEvaluateFeatureFlagUseCase_EvaluateString(t *testing.T) {
	var (
		errRepositoryFailed = errors.New("repository failed")
	)

	tests := []struct {
		name         string
		input        EvaluateFeatureFlagStringUseCaseInput
		mockResult   *domain.FeatureFlagEvaluation
		mockError    error
		assertResult func(t *testing.T, result string, err error)
	}{
		{
			name: "Success_VariantResult",
			input: EvaluateFeatureFlagStringUseCaseInput{
				FlagKey:       "theme_variant",
				EntityID:      "user_123",
				FallbackValue: "default",
			},
			mockResult: makeStringEvaluation("dark", true),
			mockError:  nil,
			assertResult: func(t *testing.T, result string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "dark", result)
			},
		},
		{
			name: "Error_VariantWithFallback",
			input: EvaluateFeatureFlagStringUseCaseInput{
				FlagKey:       "broken_variant",
				EntityID:      "user_456",
				FallbackValue: "fallback_value",
			},
			mockResult: nil,
			mockError:  errRepositoryFailed,
			assertResult: func(t *testing.T, result string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "fallback_value", result)
			},
		},
		{
			name: "Edge_VariantEvaluationFailedUseFallback",
			input: EvaluateFeatureFlagStringUseCaseInput{
				FlagKey:       "failed_variant",
				EntityID:      "user_789",
				FallbackValue: "safe_default",
			},
			mockResult: makeStringEvaluation("wrong", false),
			mockError:  nil,
			assertResult: func(t *testing.T, result string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "safe_default", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := new(mockFeatureFlagRepository)
			mockRepo.On("EvaluateString",
				mock.Anything,
				tt.input.FlagKey,
				tt.input.EntityID,
			).Return(tt.mockResult, tt.mockError).Once()

			sut := NewEvaluateFeatureFlagStringUseCase(mockRepo)
			ctx := t.Context()

			// Act
			result, err := sut.Execute(ctx, tt.input)

			// Assert
			tt.assertResult(t, result.Result, err)
			mockRepo.AssertExpectations(t)
		})
	}
}
