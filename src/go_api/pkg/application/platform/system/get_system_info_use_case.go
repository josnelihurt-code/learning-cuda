package system

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

type GetSystemInfoUseCaseInput struct{}

type GetSystemInfoUseCaseOutput struct {
	SystemInfo *domain.SystemInfo
}

type getSystemInfoUseCase struct {
	configRepo    configRepository
	buildInfoRepo buildInfoRepository
	versionRepo   versionRepository
}

func NewGetSystemInfoUseCase(
	configRepo configRepository,
	buildInfoRepo buildInfoRepository,
	versionRepo versionRepository,
) *getSystemInfoUseCase {
	return &getSystemInfoUseCase{
		configRepo:    configRepo,
		buildInfoRepo: buildInfoRepo,
		versionRepo:   versionRepo,
	}
}

func (uc *getSystemInfoUseCase) Execute(ctx context.Context, _ GetSystemInfoUseCaseInput) (GetSystemInfoUseCaseOutput, error) {
	environment := uc.configRepo.GetEnvironment()

	systemInfo := &domain.SystemInfo{
		Version: domain.SystemVersion{
			GoVersion:    uc.versionRepo.GetGoVersion(),
			ProtoVersion: uc.versionRepo.GetProtoVersion(),
			Branch:       uc.buildInfoRepo.GetBranch(),
			BuildTime:    uc.buildInfoRepo.GetBuildTime(),
			CommitHash:   uc.buildInfoRepo.GetCommitHash(),
		},
		Environment: environment,
	}

	return GetSystemInfoUseCaseOutput{SystemInfo: systemInfo}, nil
}
