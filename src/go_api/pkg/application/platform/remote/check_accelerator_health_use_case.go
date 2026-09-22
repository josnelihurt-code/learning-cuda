package remote

import "context"

type CheckAcceleratorHealthUseCaseInput struct{}

type CheckAcceleratorHealthUseCaseOutput struct {
	Healthy bool
	Message string
}

type checkAcceleratorHealthUseCase struct {
	health acceleratorHealth
}

func NewCheckAcceleratorHealthUseCase(health acceleratorHealth) *checkAcceleratorHealthUseCase {
	return &checkAcceleratorHealthUseCase{health: health}
}

func (uc *checkAcceleratorHealthUseCase) Execute(
	ctx context.Context,
	_ CheckAcceleratorHealthUseCaseInput,
) (CheckAcceleratorHealthUseCaseOutput, error) {
	_ = ctx
	if uc.health == nil || !uc.health.IsAvailable() {
		return CheckAcceleratorHealthUseCaseOutput{
			Healthy: false,
			Message: "no accelerator registered",
		}, nil
	}
	return CheckAcceleratorHealthUseCaseOutput{
		Healthy: true,
		Message: "Accelerator is healthy",
	}, nil
}
