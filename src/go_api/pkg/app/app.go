package app

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	ffapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/flags"
	imageapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/image"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	systemapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/system"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/mqtt"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/processor"
	"github.com/jrb/cuda-learning/src/go_api/pkg/interfaces/connectrpc"
	httphandlers "github.com/jrb/cuda-learning/src/go_api/pkg/interfaces/http"
	"github.com/jrb/cuda-learning/src/go_api/pkg/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type App struct {
	// Context
	appContext context.Context

	// Embed dependencies
	Deps

	// Interceptors
	interceptors []connect.Interceptor
}

type Deps struct {
	// Configuration
	Config *config.Manager

	GetSystemInfoUC       useCase[systemapp.GetSystemInfoUseCaseInput, systemapp.GetSystemInfoUseCaseOutput]
	EvaluateFFBooleanUC   useCase[ffapp.EvaluateFeatureFlagBooleanUseCaseInput, ffapp.EvaluateFeatureFlagBooleanUseCaseOutput]
	EvaluateFFStringUC    useCase[ffapp.EvaluateFeatureFlagStringUseCaseInput, ffapp.EvaluateFeatureFlagStringUseCaseOutput]
	ListInputsUC          useCase[videoapp.ListInputsUseCaseInput, videoapp.ListInputsUseCaseOutput]
	ListAvailableImagesUC useCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput]
	UploadImageUC         useCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput]
	ListVideosUC          useCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput]
	UploadVideoUC         useCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput]
	StartVideoPlaybackUC  useCase[videoapp.StartVideoPlaybackUseCaseInput, videoapp.StartVideoPlaybackUseCaseOutput]
	StopVideoPlaybackUC   useCase[videoapp.StopVideoPlaybackUseCaseInput, videoapp.StopVideoPlaybackUseCaseOutput]

	// Infrastructure
	AcceleratorGateway *processor.AcceleratorGateway
	DeviceMonitor      *mqtt.DeviceMonitor

	// Repositories
	FeatureFlagRepo featureFlagRepository
}

func New(ctx context.Context, deps Deps) (*App, error) {
	if deps.Config == nil {
		return nil, errors.New("config is required")
	}
	if deps.AcceleratorGateway == nil {
		return nil, errors.New("accelerator gateway is required")
	}
	if deps.GetSystemInfoUC == nil {
		return nil, errors.New("get system info use case is required")
	}
	if deps.EvaluateFFBooleanUC == nil {
		return nil, errors.New("evaluate feature flag boolean use case is required")
	}
	if deps.EvaluateFFStringUC == nil {
		return nil, errors.New("evaluate feature flag string use case is required")
	}
	if deps.FeatureFlagRepo == nil {
		return nil, errors.New("feature flag repository is required")
	}
	if deps.ListInputsUC == nil {
		return nil, errors.New("list inputs use case is required")
	}
	if deps.StartVideoPlaybackUC == nil {
		return nil, errors.New("start video playback use case is required")
	}
	if deps.StopVideoPlaybackUC == nil {
		return nil, errors.New("stop video playback use case is required")
	}
	if deps.ListAvailableImagesUC == nil {
		return nil, errors.New("list available images use case is required")
	}
	if deps.UploadImageUC == nil {
		return nil, errors.New("upload image use case is required")
	}
	if deps.ListVideosUC == nil {
		return nil, errors.New("list videos use case is required")
	}
	if deps.UploadVideoUC == nil {
		return nil, errors.New("upload video use case is required")
	}
	if deps.DeviceMonitor == nil {
		return nil, errors.New("MQTT device monitor is required")
	}

	app := &App{
		appContext: ctx,
		Deps:       deps,
	}

	return app, nil
}

func (a *App) makeTelemetryMiddleware(handler http.Handler) http.Handler {
	if !a.Config.IsObservabilityEnabled(a.appContext) {
		return handler
	}

	instrumentedHandler := otelhttp.NewHandler(
		handler,
		"http-server",
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	logger.Global().Info().Msg("OpenTelemetry HTTP instrumentation enabled")
	return instrumentedHandler
}

func (a *App) setupObservability(mux *http.ServeMux) {
	log := logger.Global()
	if !a.Config.IsObservabilityEnabled(a.appContext) {
		log.Info().Msg("OpenTelemetry HTTP instrumentation disabled")
		return
	}
	a.interceptors = append(a.interceptors, telemetry.TraceContextInterceptor())
	traceProxy := httphandlers.NewTraceProxyHandler(
		a.Config.Observability.OtelCollectorHTTPEndpoint,
		true,
	)
	mux.Handle("/api/traces", traceProxy)
	log.Info().Msg("Trace proxy endpoint registered at /api/traces")

	logsProxy := httphandlers.NewLogsProxyHandler(
		a.Config.Observability.OtelCollectorHTTPEndpoint,
		true,
	)
	mux.Handle("/api/logs", logsProxy)
	log.Info().Msg("Logs proxy endpoint registered at /api/logs")
}

func (a *App) setupConnectRPCServices(mux *http.ServeMux) {
	videoPlaybackHandler := connectrpc.NewVideoPlaybackHandler(
		a.StartVideoPlaybackUC,
		a.StopVideoPlaybackUC,
	)

	connectrpc.RegisterConfigService(
		mux,
		connectrpc.ConfigHandlerDeps{
			FeatureFlagRepo:     a.FeatureFlagRepo,
			ListInputsUC:        a.ListInputsUC,
			GetSystemInfoUC:     a.GetSystemInfoUC,
			EvaluateFFBooleanUC: a.EvaluateFFBooleanUC,
			EvaluateFFStringUC:  a.EvaluateFFStringUC,
			ConfigManager:       a.Config,
		},
		a.interceptors...,
	)

	connectrpc.RegisterFileService(
		mux,
		a.ListAvailableImagesUC,
		a.UploadImageUC,
		a.ListVideosUC,
		a.UploadVideoUC,
		a.interceptors...,
	)

	connectrpc.RegisterWebRTCSignalingService(
		mux,
		a.AcceleratorGateway,
		a.interceptors...,
	)

	connectrpc.RegisterVideoPlaybackService(mux, videoPlaybackHandler, a.interceptors...)

	connectrpc.RegisterRemoteManagementService(
		mux,
		a.AcceleratorGateway,
		a.Config,
		a.DeviceMonitor,
		a.interceptors...,
	)

	transcoder := connectrpc.SetupVanguardTranscoder(&connectrpc.VanguardConfig{
		VideoPlaybackHandler:  videoPlaybackHandler,
		FeatureFlagRepo:       a.FeatureFlagRepo,
		ListInputsUC:          a.ListInputsUC,
		GetSystemInfoUC:       a.GetSystemInfoUC,
		EvaluateFFBooleanUC:   a.EvaluateFFBooleanUC,
		EvaluateFFStringUC:    a.EvaluateFFStringUC,
		ConfigManager:         a.Config,
		ListAvailableImagesUC: a.ListAvailableImagesUC,
		UploadImageUC:         a.UploadImageUC,
		ListVideosUC:          a.ListVideosUC,
		UploadVideoUC:         a.UploadVideoUC,
		Interceptors:          a.interceptors,
	})

	mux.Handle("/api/", transcoder)

	logger.Global().Info().Msg("Connect-RPC handlers and Vanguard transcoder registered (REST + Connect + gRPC)")
}

func (a *App) setupHealthEndpoint(mux *http.ServeMux) {
	// Health endpoint uses plain HTTP instead of ConnectRPC because:
	// 1. Load balancers (k8s, Docker) require simple HTTP 200/503
	// 2. No protobuf complexity needed for basic health checks
	// 3. Industry standard for healthcheck endpoints
	healthHandler := httphandlers.NewHealthHandler()
	mux.Handle("/health", healthHandler)
	logger.Global().Info().Msg("Health endpoint registered at /health")
}

func (a *App) Run() error {
	log := logger.Global()
	defer func() {
		if err := a.DeviceMonitor.Stop(); err != nil {
			log.Warn().Err(err).Msg("Failed to stop MQTT device monitor")
		}
	}()

	if a.DeviceMonitor == nil {
		return errors.New("MQTT device monitor not initialized")
	}

	if err := a.DeviceMonitor.Start(a.appContext); err != nil {
		log.Err(err).Msg("Failed to start MQTT device monitor")
		return err
	}
	log.Info().Msg("MQTT device monitor started")

	mux := http.NewServeMux()
	a.setupObservability(mux)

	a.setupHealthEndpoint(mux)
	a.setupConnectRPCServices(mux)
	handler := a.makeTelemetryMiddleware(mux)

	errChan := make(chan error, 2)

	go func() {
		log.Info().
			Str("port", a.Config.Server.HTTPPort).
			Msg("Starting HTTP server")
		if err := http.ListenAndServe(a.Config.Server.HTTPPort, handler); err != nil {
			errChan <- err
		}
	}()

	if a.Config.Server.TLS.Enabled {
		go func() {
			log.Info().
				Str("port", a.Config.Server.HTTPSPort).
				Str("cert", a.Config.Server.TLS.CertFile).
				Msg("Starting HTTPS server")
			if err := http.ListenAndServeTLS(
				a.Config.Server.HTTPSPort,
				a.Config.Server.TLS.CertFile,
				a.Config.Server.TLS.KeyFile,
				handler,
			); err != nil {
				errChan <- err
			}
		}()
	}

	return <-errChan
}
