package app

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/src/go_api/pkg/application"
	ffapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/flags"
	imageapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/image"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	systemapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/system"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/featureflags"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/jrb/cuda-learning/src/go_api/pkg/interfaces/connectrpc"
	httphandlers "github.com/jrb/cuda-learning/src/go_api/pkg/interfaces/http"
	"github.com/jrb/cuda-learning/src/go_api/pkg/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
)

type App struct {
	// Context
	appContext context.Context

	// Embed dependencies
	Deps

	// Interceptors
	interceptors []connect.Interceptor
}

// AcceleratorControl is the lifecycle slice of the accelerator control server
// that app.Run owns: the control listener must be up before slow init starts.
type AcceleratorControl interface {
	Start() error
}

// AcceleratorGateway is what the Connect layer consumes from the accelerator
// gateway: signaling streams for WebRTC sessions and availability for
// accelerator health checks. Declared here because app is the wiring point
// that hands one gateway to several handlers.
type AcceleratorGateway interface {
	IsAvailable() bool
	SignalingStream(ctx context.Context) (pb.WebRTCSignalingService_SignalingStreamClient, error)
}

// DeviceMonitor is what the app consumes from the MQTT device monitor: the
// Start/Stop lifecycle driven by Run plus the power and subscription surface
// used by the remote-management Connect handler.
type DeviceMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	PowerOn() error
	Subscribe(callback func(*domain.DeviceStatus)) func()
}

type Deps struct {
	// Configuration
	Config *config.Manager

	GetSystemInfoUC       application.UseCase[systemapp.GetSystemInfoUseCaseInput, systemapp.GetSystemInfoUseCaseOutput]
	EvaluateFFBooleanUC   application.UseCase[ffapp.EvaluateFeatureFlagBooleanUseCaseInput, ffapp.EvaluateFeatureFlagBooleanUseCaseOutput]
	EvaluateFFStringUC    application.UseCase[ffapp.EvaluateFeatureFlagStringUseCaseInput, ffapp.EvaluateFeatureFlagStringUseCaseOutput]
	ListInputsUC          application.UseCase[videoapp.ListInputsUseCaseInput, videoapp.ListInputsUseCaseOutput]
	ListAvailableImagesUC application.UseCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput]
	UploadImageUC         application.UseCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput]
	ListVideosUC          application.UseCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput]
	UploadVideoUC         application.UseCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput]

	// Infrastructure
	AcceleratorControl AcceleratorControl
	AcceleratorGateway AcceleratorGateway
	DeviceMonitor      DeviceMonitor

	// Repositories
	FeatureFlagRepo *featureflags.GoffRepository
}

func New(ctx context.Context, deps Deps) (*App, error) {
	if deps.Config == nil {
		return nil, errors.New("config is required")
	}
	if deps.AcceleratorControl == nil {
		return nil, errors.New("accelerator control server is required")
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
	// Handlers are constructed once and shared by the mux registrations and
	// the Vanguard transcoder, so the Connect and REST/gRPC surfaces can
	// never diverge.
	configHandler := connectrpc.NewConfigHandler(connectrpc.ConfigHandlerDeps{
		FeatureFlagRepo:     a.FeatureFlagRepo,
		ListInputsUC:        a.ListInputsUC,
		GetSystemInfoUC:     a.GetSystemInfoUC,
		EvaluateFFBooleanUC: a.EvaluateFFBooleanUC,
		EvaluateFFStringUC:  a.EvaluateFFStringUC,
		ConfigManager:       a.Config,
	})
	fileHandler := connectrpc.NewFileHandler(
		a.ListAvailableImagesUC,
		a.UploadImageUC,
		a.ListVideosUC,
		a.UploadVideoUC,
	)
	webrtcSignalingHandler := connectrpc.NewWebRTCSignalingHandler(a.AcceleratorGateway)
	remoteManagementHandler := connectrpc.NewRemoteManagementHandler(a.AcceleratorGateway, a.Config, a.DeviceMonitor)

	connectrpc.RegisterConfigService(mux, configHandler, a.interceptors...)
	connectrpc.RegisterFileService(mux, fileHandler, a.interceptors...)
	connectrpc.RegisterWebRTCSignalingService(mux, webrtcSignalingHandler, a.interceptors...)
	connectrpc.RegisterRemoteManagementService(mux, remoteManagementHandler, a.interceptors...)

	transcoder := connectrpc.SetupVanguardTranscoder(configHandler, fileHandler, a.interceptors)
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

	// Start the accelerator control listener before the slow MQTT init below —
	// otherwise accelerators dial :60062 while the process is still blocked in
	// startup (e.g. broker connect retry).
	if err := a.AcceleratorControl.Start(); err != nil {
		log.Err(err).Msg("Failed to start accelerator control server")
		return err
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

	var g errgroup.Group

	g.Go(func() error {
		log.Info().
			Str("port", a.Config.Server.HTTPPort).
			Msg("Starting HTTP server")
		return http.ListenAndServe(a.Config.Server.HTTPPort, handler)
	})

	if a.Config.Server.TLS.Enabled {
		g.Go(func() error {
			log.Info().
				Str("port", a.Config.Server.HTTPSPort).
				Str("cert", a.Config.Server.TLS.CertFile).
				Msg("Starting HTTPS server")
			return http.ListenAndServeTLS(
				a.Config.Server.HTTPSPort,
				a.Config.Server.TLS.CertFile,
				a.Config.Server.TLS.KeyFile,
				handler,
			)
		})
	}

	return g.Wait()
}
