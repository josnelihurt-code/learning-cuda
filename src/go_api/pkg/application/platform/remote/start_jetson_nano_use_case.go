package remote

import (
	"context"
	"fmt"
)

type StartJetsonNanoUseCaseInput struct{}

type StartJetsonNanoUseCaseOutput struct {
	Success bool
	Step    string
	Message string
}

type startJetsonNanoUseCase struct {
	power devicePower
}

func NewStartJetsonNanoUseCase(power devicePower) *startJetsonNanoUseCase {
	return &startJetsonNanoUseCase{power: power}
}

func (uc *startJetsonNanoUseCase) Execute(
	ctx context.Context,
	_ StartJetsonNanoUseCaseInput,
) (StartJetsonNanoUseCaseOutput, error) {
	_ = ctx
	if uc.power == nil {
		return StartJetsonNanoUseCaseOutput{}, fmt.Errorf("device monitor not initialized")
	}
	if err := uc.power.PowerOn(); err != nil {
		return StartJetsonNanoUseCaseOutput{
			Success: false,
			Step:    "error",
			Message: fmt.Sprintf("Failed to send power command: %v", err),
		}, nil
	}
	return StartJetsonNanoUseCaseOutput{
		Success: true,
		Step:    "sent",
		Message: "POWER ON command sent successfully",
	}, nil
}
