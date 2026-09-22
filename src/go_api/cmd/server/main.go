package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/app"
	"github.com/jrb/cuda-learning/src/go_api/pkg/container"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/jrb/cuda-learning/src/go_api/pkg/telemetry"
)

func main() {
	configFile := flag.String("config", "config/config.yaml", "Path to configuration file")
	flag.Parse()

	// rootCtx is never cancelled, so shutdown deadlines derived from it survive ctx cancellation.
	rootCtx := context.Background()
	ctx, stop := signal.NotifyContext(rootCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	di, err := container.New(ctx, *configFile)
	if err != nil {
		logger.Global().Fatal().Err(err).Msg("Failed to initialize container")
	}

	log := logger.Global()

	tracerProvider, err := telemetry.New(
		ctx,
		di.Config.IsObservabilityEnabled(ctx),
		&di.Config.Observability,
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize telemetry")
	}

	meterProvider, err := telemetry.NewMeterProvider(
		ctx,
		di.Config.IsObservabilityEnabled(ctx),
		&di.Config.Observability,
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize meter provider")
	}
	shutdownWithTimeout := func(name string, shutdown func(context.Context) error) {
		shutdownCtx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msgf("Error shutting down %s", name)
		}
	}
	shutdownTelemetry := func() {
		if tracerProvider != nil {
			shutdownWithTimeout("tracer provider", tracerProvider.Shutdown)
		}
		if meterProvider != nil {
			shutdownWithTimeout("meter provider", meterProvider.Shutdown)
		}
	}

	server, err := app.New(ctx, app.Deps{
		Config:                di.Config,
		AcceleratorControl:    di.AcceleratorControl,
		AcceleratorGateway:    di.AcceleratorGateway,
		GetSystemInfoUC:       di.GetSystemInfoUseCase,
		EvaluateFFBooleanUC:   di.EvaluateFeatureFlagBooleanUseCase,
		EvaluateFFStringUC:    di.EvaluateFeatureFlagStringUseCase,
		FeatureFlagRepo:       di.FeatureFlagRepo,
		ListInputsUC:          di.ListInputsUseCase,
		ListAvailableImagesUC: di.ListAvailableImagesUseCase,
		UploadImageUC:         di.UploadImageUseCase,
		ListVideosUC:          di.ListVideosUseCase,
		UploadVideoUC:         di.UploadVideoUseCase,
		DeviceMonitor:         di.DeviceMonitor,
	})
	if err != nil {
		logger.Global().Fatal().Err(err).Msg("Failed to initialize app")
	}

	go func() {
		<-ctx.Done()
		log.Info().Msg("Received signal, shutting down gracefully")
	}()

	if err := server.Run(); err != nil {
		log.Error().Err(err).Msg("Server error")
		di.Close(rootCtx)
		shutdownTelemetry()
		os.Exit(1)
	}

	di.Close(rootCtx)
	shutdownTelemetry()
}
