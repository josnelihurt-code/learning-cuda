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

	ctx := context.Background()

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
	shutdownTelemetry := func() {
		if tracerProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
				log.Error().Err(err).Msg("Error shutting down tracer provider")
			}
			cancel()
		}
		if meterProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := meterProvider.Shutdown(shutdownCtx); err != nil {
				log.Error().Err(err).Msg("Error shutting down meter provider")
			}
			cancel()
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

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Run()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		if err != nil {
			log.Error().Err(err).Msg("Server error")
			di.Close(ctx)
			shutdownTelemetry()
			os.Exit(1)
		}
	case sig := <-sigChan:
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down gracefully")
	}

	di.Close(ctx)
	shutdownTelemetry()
}
