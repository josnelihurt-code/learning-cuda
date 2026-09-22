package system

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	tracer := otel.Tracer("get-system-info")
	_, span := tracer.Start(ctx, "GetSystemInfo",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

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

	span.SetAttributes(
		attribute.String("version.go", systemInfo.Version.GoVersion),
		attribute.String("version.proto", systemInfo.Version.ProtoVersion),
		attribute.String("version.branch", systemInfo.Version.Branch),
		attribute.String("version.build_time", systemInfo.Version.BuildTime),
		attribute.String("version.commit_hash", systemInfo.Version.CommitHash),
		attribute.String("environment", systemInfo.Environment),
	)

	return GetSystemInfoUseCaseOutput{SystemInfo: systemInfo}, nil
}
