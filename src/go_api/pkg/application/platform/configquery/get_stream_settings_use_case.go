package configquery

import "context"

type GetStreamSettingsUseCaseInput struct{}

type GetStreamSettingsUseCaseOutput struct {
	WebRTCSignalingEndpoint string
}

type getStreamSettingsUseCase struct {
	source streamSettingsSource
}

func NewGetStreamSettingsUseCase(source streamSettingsSource) *getStreamSettingsUseCase {
	return &getStreamSettingsUseCase{source: source}
}

func (uc *getStreamSettingsUseCase) Execute(
	ctx context.Context,
	_ GetStreamSettingsUseCaseInput,
) (GetStreamSettingsUseCaseOutput, error) {
	_ = ctx
	endpoint := uc.source.WebRTCSignalingEndpoint()
	if endpoint == "" {
		return GetStreamSettingsUseCaseOutput{}, ErrSignalingEndpointNotConfigured
	}
	return GetStreamSettingsUseCaseOutput{WebRTCSignalingEndpoint: endpoint}, nil
}
