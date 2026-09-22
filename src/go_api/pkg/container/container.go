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
	remoteapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/remote"
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
	"go.opentelemetry.io/otel/attribute"
)

type container struct {
	Config *config.Manager

	EvaluateFeatureFlagBooleanUseCase application.UseCase[ffapp.EvaluateFeatureFlagBooleanUseCaseInput, ffapp.EvaluateFeatureFlagBooleanUseCaseOutput]
	EvaluateFeatureFlagStringUseCase  application.UseCase[ffapp.EvaluateFeatureFlagStringUseCaseInput, ffapp.EvaluateFeatureFlagStringUseCaseOutput]
	ListFeatureFlagsUseCase           application.UseCase[ffapp.ListFeatureFlagsUseCaseInput, ffapp.ListFeatureFlagsUseCaseOutput]
	UpsertFeatureFlagUseCase          application.UseCase[ffapp.UpsertFeatureFlagUseCaseInput, ffapp.UpsertFeatureFlagUseCaseOutput]
	GetSystemInfoUseCase              application.UseCase[systemapp.GetSystemInfoUseCaseInput, systemapp.GetSystemInfoUseCaseOutput]
	ListInputsUseCase                 application.UseCase[videoapp.ListInputsUseCaseInput, videoapp.ListInputsUseCaseOutput]
	ListAvailableImagesUseCase        application.UseCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput]
	UploadImageUseCase                application.UseCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput]
	ListVideosUseCase                 application.UseCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput]
	UploadVideoUseCase                application.UseCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput]
	StartJetsonNanoUseCase            application.UseCase[remoteapp.StartJetsonNanoUseCaseInput, remoteapp.StartJetsonNanoUseCaseOutput]
	CheckAcceleratorHealthUseCase     application.UseCase[remoteapp.CheckAcceleratorHealthUseCaseInput, remoteapp.CheckAcceleratorHealthUseCaseOutput]

	AcceleratorGateway *processor.AcceleratorGateway
	AcceleratorControl *processor.ControlServer
	DeviceMonitor      *mqtt.DeviceMonitor
}

func New(ctx context.Context, configFile string) (*container, error) {
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
	log.Info().Str("config_file", configFile).Any("config", cfg.Redacted()).Msg("container initialized")

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

	evaluateFFBooleanUseCase := application.WithTrace(
		"evaluate-feature-flag",
		"EvaluateBoolean",
		ffapp.NewEvaluateFeatureFlagBooleanUseCase(featureFlagRepo),
		func(in ffapp.EvaluateFeatureFlagBooleanUseCaseInput) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("flag.key", in.FlagKey),
				attribute.String("flag.entity_id", in.EntityID),
				attribute.Bool("flag.fallback_value", in.FallbackValue),
			}
		},
		func(in ffapp.EvaluateFeatureFlagBooleanUseCaseInput, out ffapp.EvaluateFeatureFlagBooleanUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.Bool("flag.result", out.Result),
				attribute.Bool("flag.fallback_value", in.FallbackValue),
			}
		},
	)
	evaluateFFStringUseCase := application.WithTrace(
		"evaluate-feature-flag",
		"EvaluateString",
		ffapp.NewEvaluateFeatureFlagStringUseCase(featureFlagRepo),
		func(in ffapp.EvaluateFeatureFlagStringUseCaseInput) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("flag.key", in.FlagKey),
				attribute.String("flag.entity_id", in.EntityID),
				attribute.String("flag.fallback_value", in.FallbackValue),
			}
		},
		func(_ ffapp.EvaluateFeatureFlagStringUseCaseInput, out ffapp.EvaluateFeatureFlagStringUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("flag.result", out.Result),
			}
		},
	)
	listFeatureFlagsUseCase := application.WithTrace(
		"feature-flag-admin",
		"ListFeatureFlags",
		ffapp.NewListFeatureFlagsUseCase(featureFlagRepo),
		nil,
		func(_ ffapp.ListFeatureFlagsUseCaseInput, out ffapp.ListFeatureFlagsUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.Int("flags.count", len(out.Flags))}
		},
	)
	upsertFeatureFlagUseCase := application.WithTrace(
		"feature-flag-admin",
		"UpsertFeatureFlag",
		ffapp.NewUpsertFeatureFlagUseCase(featureFlagRepo),
		func(in ffapp.UpsertFeatureFlagUseCaseInput) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("flag.key", in.Key),
				attribute.String("flag.type", string(in.Type)),
			}
		},
		nil,
	)

	getSystemInfoUseCase := application.WithTrace(
		"get-system-info",
		"GetSystemInfo",
		systemapp.NewGetSystemInfoUseCase(configRepo, buildInfoRepo, versionRepo),
		nil,
		func(_ systemapp.GetSystemInfoUseCaseInput, out systemapp.GetSystemInfoUseCaseOutput, _ error) []attribute.KeyValue {
			if out.SystemInfo == nil {
				return nil
			}
			return []attribute.KeyValue{
				attribute.String("version.go", out.SystemInfo.Version.GoVersion),
				attribute.String("version.proto", out.SystemInfo.Version.ProtoVersion),
				attribute.String("version.branch", out.SystemInfo.Version.Branch),
				attribute.String("version.build_time", out.SystemInfo.Version.BuildTime),
				attribute.String("version.commit_hash", out.SystemInfo.Version.CommitHash),
				attribute.String("environment", out.SystemInfo.Environment),
			}
		},
	)

	videoRepo := video.NewFileVideoRepository(ctx, "data/videos", "data/video_previews")
	cameraSource := processor.NewRegistryCameraSource(registry)
	listInputsUseCase := application.WithTrace(
		"list-inputs",
		"ListInputs",
		videoapp.NewListInputsUseCase(videoRepo, cameraSource),
		nil,
		func(_ videoapp.ListInputsUseCaseInput, out videoapp.ListInputsUseCaseOutput, _ error) []attribute.KeyValue {
			var staticCount, cameraCount, videoCount, remoteCameraCount int
			for _, src := range out.Inputs {
				switch src.Type {
				case "static":
					staticCount++
				case "camera":
					cameraCount++
				case "video":
					videoCount++
				case "remote_camera":
					remoteCameraCount++
				}
			}
			return []attribute.KeyValue{
				attribute.Int("input_sources.count", len(out.Inputs)),
				attribute.Int("input_sources.static_count", staticCount),
				attribute.Int("input_sources.camera_count", cameraCount),
				attribute.Int("input_sources.video_count", videoCount),
				attribute.Int("input_sources.remote_camera_count", remoteCameraCount),
			}
		},
	)

	staticImageRepo := filesystem.NewStaticImageRepository(cfg.StaticImages.Directory)
	listAvailableImagesUseCase := application.WithTrace(
		"list-available-images",
		"ListAvailableImagesUseCase.Execute",
		imageapp.NewListAvailableImagesUseCase(staticImageRepo), //nolint:language
		nil,
		func(_ imageapp.ListAvailableImagesUseCaseInput, out imageapp.ListAvailableImagesUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.Int("images.count", len(out.Images)),
			}
		},
	)
	uploadImageUseCase := application.WithTrace(
		"upload-image",
		"UploadImageUseCase.Execute",
		imageapp.NewUploadImageUseCase(staticImageRepo),
		func(in imageapp.UploadImageUseCaseInput) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("filename", in.Filename),
				attribute.Int("file_size", len(in.FileData)),
			}
		},
		func(_ imageapp.UploadImageUseCaseInput, out imageapp.UploadImageUseCaseOutput, err error) []attribute.KeyValue {
			attrs := make([]attribute.KeyValue, 0, 3)
			switch {
			case errors.Is(err, imageapp.ErrFileTooLarge):
				attrs = append(attrs, attribute.String("validation.error", "file_too_large"))
			case errors.Is(err, imageapp.ErrInvalidFormat):
				attrs = append(attrs, attribute.String("validation.error", "invalid_format"))
			}
			if out.Image != nil {
				attrs = append(attrs,
					attribute.String("image.id", out.Image.ID),
					attribute.String("image.path", out.Image.Path),
				)
			}
			return attrs
		},
	)

	listVideosUseCase := application.WithTrace(
		"list-videos",
		"ListVideos",
		videoapp.NewListVideosUseCase(videoRepo),
		nil,
		func(_ videoapp.ListVideosUseCaseInput, out videoapp.ListVideosUseCaseOutput, err error) []attribute.KeyValue {
			if err != nil {
				return []attribute.KeyValue{attribute.Bool("error", true)}
			}
			defaultCount := 0
			for _, vid := range out.Videos {
				if vid.IsDefault {
					defaultCount++
				}
			}
			return []attribute.KeyValue{
				attribute.Int("videos.count", len(out.Videos)),
				attribute.Int("videos.default_count", defaultCount),
			}
		},
	)
	videoStorage := video.NewFileVideoStorage("data/videos", "/data/videos")
	videoPreviewGenerator := video.NewFilePreviewGenerator("data/video_previews", "/data/video_previews")
	uploadVideoUseCase := application.WithTrace(
		"upload-video",
		"UploadVideo",
		videoapp.NewUploadVideoUseCase(videoRepo, videoStorage, videoPreviewGenerator),
		func(in videoapp.UploadVideoUseCaseInput) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("filename", in.Filename),
				attribute.Int("file_size", len(in.FileData)),
			}
		},
		func(_ videoapp.UploadVideoUseCaseInput, out videoapp.UploadVideoUseCaseOutput, err error) []attribute.KeyValue {
			attrs := make([]attribute.KeyValue, 0, 3)
			switch {
			case errors.Is(err, videoapp.ErrInvalidFormat):
				attrs = append(attrs, attribute.Bool("error.invalid_format", true))
			case errors.Is(err, videoapp.ErrFileTooLarge):
				attrs = append(attrs, attribute.Bool("error.file_too_large", true))
			case err != nil:
				attrs = append(attrs, attribute.Bool("error", true))
			}
			if out.Video != nil {
				attrs = append(attrs,
					attribute.String("video.id", out.Video.ID),
					attribute.Bool("upload.success", true),
				)
			}
			return attrs
		},
	)

	deviceMonitor := mqtt.NewDeviceMonitor(ctx, cfg.MQTT)
	startJetsonNanoUseCase := application.WithTrace(
		"remote-management",
		"StartJetsonNano",
		remoteapp.NewStartJetsonNanoUseCase(deviceMonitor),
		nil,
		func(_ remoteapp.StartJetsonNanoUseCaseInput, out remoteapp.StartJetsonNanoUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.Bool("jetson.success", out.Success),
				attribute.String("jetson.step", out.Step),
			}
		},
	)
	checkAcceleratorHealthUseCase := application.WithTrace(
		"remote-management",
		"CheckAcceleratorHealth",
		remoteapp.NewCheckAcceleratorHealthUseCase(acceleratorGateway),
		nil,
		func(_ remoteapp.CheckAcceleratorHealthUseCaseInput, out remoteapp.CheckAcceleratorHealthUseCaseOutput, _ error) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.Bool("accelerator.healthy", out.Healthy)}
		},
	)

	return &container{
		Config:                            cfg,
		EvaluateFeatureFlagBooleanUseCase: evaluateFFBooleanUseCase,
		EvaluateFeatureFlagStringUseCase:  evaluateFFStringUseCase,
		ListFeatureFlagsUseCase:           listFeatureFlagsUseCase,
		UpsertFeatureFlagUseCase:          upsertFeatureFlagUseCase,
		GetSystemInfoUseCase:              getSystemInfoUseCase,
		ListInputsUseCase:                 listInputsUseCase,
		ListAvailableImagesUseCase:        listAvailableImagesUseCase,
		UploadImageUseCase:                uploadImageUseCase,
		ListVideosUseCase:                 listVideosUseCase,
		UploadVideoUseCase:                uploadVideoUseCase,
		StartJetsonNanoUseCase:            startJetsonNanoUseCase,
		CheckAcceleratorHealthUseCase:     checkAcceleratorHealthUseCase,
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

func (c *container) Close(ctx context.Context) error {
	if c.AcceleratorControl != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c.AcceleratorControl.Stop(stopCtx)
	}
	return nil
}
