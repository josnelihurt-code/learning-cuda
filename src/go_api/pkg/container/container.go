package container

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/application"
	ffapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/flags"
	imageapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/image"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	systemapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/system"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/build"
	configrepo "github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/featureflags"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/filesystem"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/mqtt"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/processor"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/version"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/video"
)

type Container struct {
	Config *config.Manager

	FeatureFlagRepo *featureflags.GoffRepository

	EvaluateFeatureFlagBooleanUseCase application.UseCase[ffapp.EvaluateFeatureFlagBooleanUseCaseInput, ffapp.EvaluateFeatureFlagBooleanUseCaseOutput]
	EvaluateFeatureFlagStringUseCase  application.UseCase[ffapp.EvaluateFeatureFlagStringUseCaseInput, ffapp.EvaluateFeatureFlagStringUseCaseOutput]
	GetSystemInfoUseCase              application.UseCase[systemapp.GetSystemInfoUseCaseInput, systemapp.GetSystemInfoUseCaseOutput]
	ListInputsUseCase                 application.UseCase[videoapp.ListInputsUseCaseInput, videoapp.ListInputsUseCaseOutput]
	ListAvailableImagesUseCase        application.UseCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput]
	UploadImageUseCase                application.UseCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput]
	ListVideosUseCase                 application.UseCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput]
	UploadVideoUseCase                application.UseCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput]

	AcceleratorGateway *processor.AcceleratorGateway
	AcceleratorControl *processor.ControlServer
	DeviceMonitor      *mqtt.DeviceMonitor
}

func New(ctx context.Context, configFile string) (*Container, error) {
	cfg := config.New(configFile)

	log := logger.New(&logger.Config{
		Level:             cfg.Logging.Level,
		Format:            cfg.Logging.Format,
		Output:            cfg.Logging.Output,
		FilePath:          cfg.Logging.FilePath,
		IncludeCaller:     cfg.Logging.IncludeCaller,
		RemoteEnabled:     cfg.Logging.RemoteEnabled,
		RemoteEndpoint:    cfg.Logging.RemoteEndpoint,
		RemoteEnvironment: cfg.Logging.RemoteEnvironment,
		RemoteAuthHeader:  cfg.Observability.AuthHeader(),
		ServiceName:       cfg.Observability.ServiceName,
	})
	log.Info().Str("config_file", configFile).Any("config", cfg.Redacted()).Msg("Container initialized")

	// Feature flags are a hard dependency of the wired handlers; there is no degraded mode.
	if err := validateFeatureFlagConfig(cfg); err != nil {
		return nil, err
	}

	featureFlagRepo := featureflags.NewGoffRepository(cfg.GoFeatureFlag.FilePath)
	if err := featureFlagRepo.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid go feature flag config: %w", err)
	}

	buildInfo := build.NewBuildInfo()

	// Create repository implementations
	configRepo := configrepo.NewConfigRepository(cfg)
	buildInfoRepo := build.NewBuildInfoRepository(buildInfo)
	versionRepo := version.NewVersionRepository()

	log.Info().
		Str("go_version", versionRepo.GetGoVersion()).
		Str("git_commit", buildInfo.CommitHash).
		Str("git_branch", buildInfo.Branch).
		Str("build_time", buildInfo.BuildTime).
		Msg("Go API starting")

	registry := processor.NewRegistry(log)

	controlServer, err := processor.NewControlServer(cfg.Processor, registry)
	if err != nil {
		err = fmt.Errorf("control server not initialized (certs missing?); accelerator connections will fail: %w", err)
		return nil, err
	}
	log.Info().
		Str("listen_address", cfg.Processor.ListenAddress).
		Msg("accelerator control server created")
	// Side-effect free by design: app.Run starts the listener before slow init.

	acceleratorGateway := processor.NewAcceleratorGateway(processor.AcceleratorGatewayConfig{
		Registry: registry,
	})

	evaluateFFBooleanUseCase := ffapp.NewEvaluateFeatureFlagBooleanUseCase(featureFlagRepo)
	evaluateFFStringUseCase := ffapp.NewEvaluateFeatureFlagStringUseCase(featureFlagRepo)

	getSystemInfoUseCase := systemapp.NewGetSystemInfoUseCase(configRepo, buildInfoRepo, versionRepo)

	videoRepo := video.NewFileVideoRepository(ctx, "data/videos", "data/video_previews")
	cameraRepo := processor.NewRegistryCameraRepository(registry)
	listInputsUseCase := videoapp.NewListInputsUseCase(videoRepo, cameraRepo)

	staticImageRepo := filesystem.NewStaticImageRepository(cfg.StaticImages.Directory)
	listAvailableImagesUseCase := imageapp.NewListAvailableImagesUseCase(staticImageRepo) //nolint:language
	uploadImageUseCase := imageapp.NewUploadImageUseCase(staticImageRepo)

	listVideosUseCase := videoapp.NewListVideosUseCase(videoRepo)
	videoStorageRepository := video.NewFileVideoStorageRepository("data/videos", "/data/videos")
	videoPreviewGeneratorRepository := video.NewFilePreviewGeneratorRepository("data/video_previews", "/data/video_previews")
	uploadVideoUseCase := videoapp.NewUploadVideoUseCase(videoRepo, videoStorageRepository, videoPreviewGeneratorRepository)

	deviceMonitor := mqtt.NewDeviceMonitor(ctx, cfg.MQTT)

	return &Container{
		Config:                            cfg,
		FeatureFlagRepo:                   featureFlagRepo,
		EvaluateFeatureFlagBooleanUseCase: evaluateFFBooleanUseCase,
		EvaluateFeatureFlagStringUseCase:  evaluateFFStringUseCase,
		GetSystemInfoUseCase:              getSystemInfoUseCase,
		ListInputsUseCase:                 listInputsUseCase,
		ListAvailableImagesUseCase:        listAvailableImagesUseCase,
		UploadImageUseCase:                uploadImageUseCase,
		ListVideosUseCase:                 listVideosUseCase,
		UploadVideoUseCase:                uploadVideoUseCase,
		AcceleratorGateway:                acceleratorGateway,
		AcceleratorControl:                controlServer,
		DeviceMonitor:                     deviceMonitor,
	}, nil
}

func validateFeatureFlagConfig(cfg *config.Manager) error {
	if !cfg.GoFeatureFlag.Enabled {
		return errors.New("feature flags are required by the API handlers but go_feature_flag.enabled is false; enable GoFeatureFlag in the config to boot")
	}
	return nil
}

func (c *Container) Close(ctx context.Context) error {
	if c.AcceleratorControl != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c.AcceleratorControl.Stop(stopCtx)
	}
	return nil
}
