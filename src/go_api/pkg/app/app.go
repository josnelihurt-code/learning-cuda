package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

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

type app struct {
	// Context
	appContext context.Context

	// Embed dependencies
	Deps

	// Interceptors
	interceptors []connect.Interceptor
}

// acceleratorControl is the lifecycle slice of the accelerator control server
// that app.Run owns: the control listener must be up before slow init starts.
type acceleratorControl interface {
	Start() error
}

// acceleratorGateway is what the Connect layer consumes from the accelerator
// gateway: signaling streams for WebRTC sessions and availability for
// accelerator health checks. Declared here because app is the wiring point
// that hands one gateway to several handlers.
type acceleratorGateway interface {
	IsAvailable() bool
	SignalingStream(ctx context.Context) (pb.WebRTCSignalingService_SignalingStreamClient, error)
}

// deviceMonitor is what the app consumes from the MQTT device monitor: the
// Start/Stop lifecycle driven by Run plus the power and subscription surface
// used by the remote-management Connect handler.
type deviceMonitor interface {
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
	AcceleratorControl acceleratorControl
	AcceleratorGateway acceleratorGateway
	DeviceMonitor      deviceMonitor

	// Repositories
	FeatureFlagRepo *featureflags.GoffRepository
}

func New(ctx context.Context, deps Deps) (*app, error) {
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

	instance := &app{
		appContext: ctx,
		Deps:       deps,
	}

	return instance, nil
}

func (a *app) makeTelemetryMiddleware(handler http.Handler) http.Handler {
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

func (a *app) setupObservability(mux *http.ServeMux) {
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

func (a *app) setupConnectRPCServices(mux *http.ServeMux) {
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

	transcoder := connectrpc.SetupVanguardTranscoder(
		configHandler,
		fileHandler,
		webrtcSignalingHandler,
		remoteManagementHandler,
		a.interceptors,
	)
	mux.Handle("/api/", transcoder)

	logger.Global().Info().Msg("Connect-RPC handlers and Vanguard transcoder registered (REST + Connect + gRPC)")
}

func (a *app) setupHealthEndpoint(mux *http.ServeMux) {
	// Health endpoint uses plain HTTP instead of ConnectRPC because:
	// 1. Load balancers (k8s, Docker) require simple HTTP 200/503
	// 2. No protobuf complexity needed for basic health checks
	// 3. Industry standard for healthcheck endpoints
	healthHandler := httphandlers.NewHealthHandler()
	mux.Handle("/health", healthHandler)
	logger.Global().Info().Msg("Health endpoint registered at /health")
}

// drainTimeout bounds how long Shutdown waits for in-flight HTTP requests after cancellation.
const drainTimeout = 10 * time.Second

type gracefulServer struct {
	server *http.Server
	serve  func() error
}

// serveWithGracefulShutdown runs each server and, once ctx is cancelled,
// drains them all with a bounded Shutdown. http.ErrServerClosed is the
// clean-shutdown success path and is filtered.
func serveWithGracefulShutdown(ctx context.Context, servers ...gracefulServer) error {
	// Derive the group context so a serve error cancels the watcher too —
	// otherwise Wait would block forever on a ctx that never fires.
	g, ctx := errgroup.WithContext(ctx)

	for _, s := range servers {
		g.Go(func() error {
			if err := s.serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
	}

	g.Go(func() error {
		<-ctx.Done()
		// Drain with a fresh bounded context — ctx is already cancelled.
		drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		var firstErr error
		for _, s := range servers {
			if err := s.server.Shutdown(drainCtx); err != nil {
				logger.Global().Warn().Err(err).Msg("HTTP server drain failed or timed out")
				// Keep draining the rest so one failure cannot leave siblings open.
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	})

	return g.Wait()
}

func (a *app) Run() error {
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

	// Explicit http.Server values on pre-bound listeners so Run can drain
	// them on cancellation instead of being cut by the process exiting.
	httpServer := &http.Server{
		Addr:    a.Config.Server.HTTPPort,
		Handler: handler,
	}
	httpListener, err := net.Listen("tcp", a.Config.Server.HTTPPort)
	if err != nil {
		log.Err(err).Str("port", a.Config.Server.HTTPPort).Msg("Failed to bind HTTP listener")
		return err
	}
	servers := []gracefulServer{
		{
			server: httpServer,
			serve: func() error {
				log.Info().
					Str("port", a.Config.Server.HTTPPort).
					Msg("Starting HTTP server")
				return httpServer.Serve(httpListener)
			},
		},
	}

	if a.Config.Server.TLS.Enabled {
		httpsServer := &http.Server{
			Addr:    a.Config.Server.HTTPSPort,
			Handler: handler,
		}
		httpsListener, err := net.Listen("tcp", a.Config.Server.HTTPSPort)
		if err != nil {
			log.Err(err).Str("port", a.Config.Server.HTTPSPort).Msg("Failed to bind HTTPS listener")
			return err
		}
		certFile := a.Config.Server.TLS.CertFile
		keyFile := a.Config.Server.TLS.KeyFile
		servers = append(servers, gracefulServer{
			server: httpsServer,
			serve: func() error {
				log.Info().
					Str("port", a.Config.Server.HTTPSPort).
					Str("cert", certFile).
					Msg("Starting HTTPS server")
				return httpsServer.ServeTLS(httpsListener, certFile, keyFile)
			},
		})
	}

	return serveWithGracefulShutdown(a.appContext, servers...)
}
