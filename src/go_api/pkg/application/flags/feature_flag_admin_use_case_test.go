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

func TestListFeatureFlagsUseCase_Execute(t *testing.T) {
	errListFailed := errors.New("list failed")

	tests := []struct {
		name         string
		mockFlags    []domain.FeatureFlag
		mockError    error
		assertResult func(t *testing.T, out ListFeatureFlagsUseCaseOutput, err error)
	}{
		{
			name: "Success_ReturnsFlags",
			mockFlags: []domain.FeatureFlag{
				{Key: "a", Name: "A", Type: domain.BooleanFlagType, Enabled: true, DefaultValue: true},
			},
			assertResult: func(t *testing.T, out ListFeatureFlagsUseCaseOutput, err error) {
				assert.NoError(t, err)
				require.Len(t, out.Flags, 1)
				assert.Equal(t, "a", out.Flags[0].Key)
			},
		},
		{
			name:      "Error_RepositoryFailed",
			mockError: errListFailed,
			assertResult: func(t *testing.T, out ListFeatureFlagsUseCaseOutput, err error) {
				assert.ErrorIs(t, err, errListFailed)
				assert.Empty(t, out.Flags)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := new(mockFeatureFlagRepository)
			mockRepo.On("ListFlags", mock.Anything).Return(tt.mockFlags, tt.mockError).Once()
			sut := NewListFeatureFlagsUseCase(mockRepo)

			// Act
			out, err := sut.Execute(context.Background(), ListFeatureFlagsUseCaseInput{})

			// Assert
			tt.assertResult(t, out, err)
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestUpsertFeatureFlagUseCase_Execute(t *testing.T) {
	errUpsertFailed := errors.New("upsert failed")

	tests := []struct {
		name         string
		input        UpsertFeatureFlagUseCaseInput
		setupMock    func(m *mockFeatureFlagRepository)
		assertResult func(t *testing.T, err error)
	}{
		{
			name: "Success_BooleanFlag",
			input: UpsertFeatureFlagUseCaseInput{
				Key:                "feature_x",
				Name:               "Feature X",
				Type:               domain.BooleanFlagType,
				Enabled:            true,
				DefaultValueString: "true",
				Description:        "desc",
			},
			setupMock: func(m *mockFeatureFlagRepository) {
				m.On("UpsertFlag", mock.Anything, domain.FeatureFlag{
					Key:          "feature_x",
					Name:         "Feature X",
					Type:         domain.BooleanFlagType,
					Enabled:      true,
					DefaultValue: true,
					Description:  "desc",
				}).Return(nil).Once()
			},
			assertResult: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Error_InvalidBooleanDefault",
			input: UpsertFeatureFlagUseCaseInput{
				Key:                "feature_x",
				Type:               domain.BooleanFlagType,
				DefaultValueString: "not-a-bool",
			},
			setupMock: func(m *mockFeatureFlagRepository) {},
			assertResult: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrInvalidBooleanDefault)
			},
		},
		{
			name: "Error_RepositoryFailed",
			input: UpsertFeatureFlagUseCaseInput{
				Key:                "feature_y",
				Type:               domain.StringFlagType,
				DefaultValueString: "hello",
			},
			setupMock: func(m *mockFeatureFlagRepository) {
				m.On("UpsertFlag", mock.Anything, mock.Anything).Return(errUpsertFailed).Once()
			},
			assertResult: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, errUpsertFailed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := new(mockFeatureFlagRepository)
			tt.setupMock(mockRepo)
			sut := NewUpsertFeatureFlagUseCase(mockRepo)

			// Act
			_, err := sut.Execute(context.Background(), tt.input)

			// Assert
			tt.assertResult(t, err)
			mockRepo.AssertExpectations(t)
		})
	}
}
