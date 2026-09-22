package flags

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

var ErrInvalidBooleanDefault = errors.New("invalid boolean default value")

type UpsertFeatureFlagUseCaseInput struct {
	Key                string
	Name               string
	Type               domain.FeatureFlagType
	Enabled            bool
	DefaultValueString string
	Description        string
}

type UpsertFeatureFlagUseCaseOutput struct{}

type upsertFeatureFlagUseCase struct {
	admin featureFlagAdmin
}

func NewUpsertFeatureFlagUseCase(admin featureFlagAdmin) *upsertFeatureFlagUseCase {
	return &upsertFeatureFlagUseCase{admin: admin}
}

func (uc *upsertFeatureFlagUseCase) Execute(
	ctx context.Context,
	input UpsertFeatureFlagUseCaseInput,
) (UpsertFeatureFlagUseCaseOutput, error) {
	defaultValue := any(input.DefaultValueString)
	if input.Type == domain.BooleanFlagType {
		parsed, err := strconv.ParseBool(input.DefaultValueString)
		if err != nil {
			return UpsertFeatureFlagUseCaseOutput{}, fmt.Errorf("%w: %w", ErrInvalidBooleanDefault, err)
		}
		defaultValue = parsed
	}

	err := uc.admin.UpsertFlag(ctx, domain.FeatureFlag{
		Key:          input.Key,
		Name:         input.Name,
		Type:         input.Type,
		Enabled:      input.Enabled,
		DefaultValue: defaultValue,
		Description:  input.Description,
	})
	if err != nil {
		return UpsertFeatureFlagUseCaseOutput{}, err
	}
	return UpsertFeatureFlagUseCaseOutput{}, nil
}
