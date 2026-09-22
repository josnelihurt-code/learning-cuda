package flags

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

type ListFeatureFlagsUseCaseInput struct{}

type ListFeatureFlagsUseCaseOutput struct {
	Flags []domain.FeatureFlag
}

type listFeatureFlagsUseCase struct {
	admin featureFlagAdmin
}

func NewListFeatureFlagsUseCase(admin featureFlagAdmin) *listFeatureFlagsUseCase {
	return &listFeatureFlagsUseCase{admin: admin}
}

func (uc *listFeatureFlagsUseCase) Execute(
	ctx context.Context,
	_ ListFeatureFlagsUseCaseInput,
) (ListFeatureFlagsUseCaseOutput, error) {
	flags, err := uc.admin.ListFlags(ctx)
	if err != nil {
		return ListFeatureFlagsUseCaseOutput{}, err
	}
	return ListFeatureFlagsUseCaseOutput{Flags: flags}, nil
}
