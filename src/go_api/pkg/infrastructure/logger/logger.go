package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/version"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

var globalLogger zerolog.Logger
var globalLocalLogger zerolog.Logger

type Config struct {
	Level             string
	Format            string
	Output            string
	FilePath          string
	IncludeCaller     bool
	RemoteEnabled     bool
	RemoteEndpoint    string
	RemoteEnvironment string
	RemoteAuthHeader  string
	ServiceName       string
}

func New(cfg *Config) zerolog.Logger {
	var output io.Writer = os.Stdout

	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano

	if cfg.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	output = buildOutputWriter(cfg, output)
	otlpHook := buildOTLPHook(cfg)

	logger := zerolog.New(output).
		Level(level).
		With().
		Timestamp()

	loggerCtx := configureCaller(&logger, cfg)

	globalLocalLogger = loggerCtx.Logger()
	globalLogger = globalLocalLogger
	if otlpHook != nil {
		globalLogger = globalLogger.Hook(otlpHook)
	}

	return globalLogger
}

func buildOutputWriter(cfg *Config, defaultOutput io.Writer) io.Writer {
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if cfg.Output == "file" && cfg.FilePath != "" {
		file, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			writers = append(writers, file)
		}
	}

	if len(writers) > 1 {
		return io.MultiWriter(writers...)
	}
	if len(writers) == 1 {
		return writers[0]
	}
	return defaultOutput
}

func buildOTLPHook(cfg *Config) zerolog.Hook {
	if !cfg.RemoteEnabled || cfg.RemoteEndpoint == "" {
		return nil
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "go-app"
	}
	serviceVersion := version.NewVersionRepository().GetGoVersion()
	if serviceVersion == "" || serviceVersion == "unknown" {
		serviceVersion = "1.0.0"
	}

	hook, err := NewOTLPHook(cfg.RemoteEndpoint, cfg.RemoteEnvironment, serviceName, serviceVersion, cfg.RemoteAuthHeader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to initialize OTLP hook: %v\n", err)
		return nil
	}
	return hook
}

func configureCaller(loggerCtx *zerolog.Context, cfg *Config) zerolog.Context {
	if cfg.IncludeCaller {
		zerolog.CallerFieldName = "caller"
		zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
			return filepath.Base(fmt.Sprintf("%s:%d", file, line))
		}
		return loggerCtx.CallerWithSkipFrameCount(1)
	}
	return *loggerCtx
}

func Global() *zerolog.Logger {
	return &globalLogger
}

// LocalOnly returns a logger that writes only to local outputs (stdout/file).
// Use this when remote logging would create a feedback loop, such as in the
// OTLP proxy handler that forwards logs to the collector.
func LocalOnly() *zerolog.Logger {
	return &globalLocalLogger
}

func FromContext(ctx context.Context) *zerolog.Logger {
	spanContext := trace.SpanContextFromContext(ctx)

	if !spanContext.IsValid() {
		return &globalLogger
	}

	logger := globalLogger.With().
		Str("trace_id", spanContext.TraceID().String()).
		Str("span_id", spanContext.SpanID().String()).
		Logger()

	return &logger
}

