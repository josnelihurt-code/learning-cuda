package connectrpc

import (
	"context"
	"fmt"
	"strconv"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	ffapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/flags"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	systemapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/system"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ConfigHandler struct {
	ConfigHandlerDeps
}

// ConfigHandlerDeps groups all dependencies needed to create a ConfigHandler.
type ConfigHandlerDeps struct {
	// Use Cases
	ListInputsUC        useCase[videoapp.ListInputsUseCaseInput, videoapp.ListInputsUseCaseOutput]
	EvaluateFFBooleanUC useCase[ffapp.EvaluateFeatureFlagBooleanUseCaseInput, ffapp.EvaluateFeatureFlagBooleanUseCaseOutput]
	EvaluateFFStringUC  useCase[ffapp.EvaluateFeatureFlagStringUseCaseInput, ffapp.EvaluateFeatureFlagStringUseCaseOutput]
	GetSystemInfoUC     useCase[systemapp.GetSystemInfoUseCaseInput, systemapp.GetSystemInfoUseCaseOutput]
	// Repositories
	FeatureFlagRepo featureFlagRepository
	// Managers
	ConfigManager *config.Manager
}

func NewConfigHandler(deps ConfigHandlerDeps) *ConfigHandler {
	return &ConfigHandler{
		ConfigHandlerDeps: deps,
	}
}

func (h *ConfigHandler) GetStreamConfig(
	ctx context.Context,
	req *connect.Request[pb.GetStreamConfigRequest],
) (*connect.Response[pb.GetStreamConfigResponse], error) {
	span := trace.SpanFromContext(ctx)
	_ = req

	if h.ConfigManager == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("config manager not available"))
	}

	if h.ConfigManager.Server.WebRTCSignalingEndpoint == "" {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("webrtc signaling endpoint not configured"))
	}

	logLevelResolver := func() string {
		const defaultLogLevel = "INFO"
		if h.EvaluateFFStringUC == nil {
			logger.FromContext(ctx).Warn().Msg("frontend log level flag resolver missing; using default")
			return defaultLogLevel
		}
		logLevel, err := h.EvaluateFFStringUC.Execute(ctx, ffapp.EvaluateFeatureFlagStringUseCaseInput{
			FlagKey:       "frontend_log_level",
			EntityID:      "default",
			FallbackValue: defaultLogLevel,
		})
		if err != nil || logLevel.Result == "" {
			logLevel.Result = defaultLogLevel
		}
		return logLevel.Result
	}

	consoleLoggingResolver := func() bool {
		if h.EvaluateFFBooleanUC == nil {
			logger.FromContext(ctx).Warn().Msg("frontend console logging flag resolver missing; using default")
			return true
		}
		consoleLogging, err := h.EvaluateFFBooleanUC.Execute(ctx, ffapp.EvaluateFeatureFlagBooleanUseCaseInput{
			FlagKey:       "frontend_console_logging",
			EntityID:      "default",
			FallbackValue: true,
		})
		if err != nil {
			consoleLogging.Result = true
		}
		return consoleLogging.Result
	}

	// Return WebRTC signaling endpoint via ConnectRPC
	logLevel := logLevelResolver()
	consoleLogging := consoleLoggingResolver()
	endpoints := []*pb.StreamEndpoint{
		{
			Type:           "webrtc",
			Endpoint:       h.ConfigManager.Server.WebRTCSignalingEndpoint,
			LogLevel:       logLevelResolver(),
			ConsoleLogging: consoleLoggingResolver(),
		},
	}

	logger.FromContext(ctx).Debug().Bool("console_logging", consoleLogging).Msg("StreamEndpoint console_logging value")

	span.SetAttributes(
		attribute.String("config.endpoint", h.ConfigManager.Server.WebRTCSignalingEndpoint),
		attribute.String("config.log_level", logLevel),
		attribute.Bool("config.console_logging", consoleLogging),
		attribute.Int("config.endpoint_count", len(endpoints)),
	)

	return connect.NewResponse(&pb.GetStreamConfigResponse{
		Endpoints: endpoints,
	}), nil
}

func (h *ConfigHandler) ListFeatureFlags(
	ctx context.Context,
	req *connect.Request[pb.ListFeatureFlagsRequest],
) (*connect.Response[pb.ListFeatureFlagsResponse], error) {
	_ = req
	if h.FeatureFlagRepo == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("feature flags repository not available"))
	}
	flags, err := h.FeatureFlagRepo.ListFlags(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := make([]*pb.ManagedFeatureFlag, 0, len(flags))
	for _, flag := range flags {
		result = append(result, &pb.ManagedFeatureFlag{
			Key:          flag.Key,
			Name:         flag.Name,
			Type:         string(flag.Type),
			Enabled:      flag.Enabled,
			DefaultValue: fmt.Sprintf("%v", flag.DefaultValue),
			Description:  flag.Description,
		})
	}
	return connect.NewResponse(&pb.ListFeatureFlagsResponse{Flags: result}), nil
}

func (h *ConfigHandler) UpsertFeatureFlag(
	ctx context.Context,
	req *connect.Request[pb.UpsertFeatureFlagRequest],
) (*connect.Response[pb.UpsertFeatureFlagResponse], error) {
	if req.Msg.GetFlag() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("flag is required"))
	}
	if h.FeatureFlagRepo == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("feature flags repository not available"))
	}
	in := req.Msg.GetFlag()
	flagType := domain.FeatureFlagType(in.GetType())
	defaultValue := interface{}(in.GetDefaultValue())
	if flagType == domain.BooleanFlagType {
		parsed, err := strconv.ParseBool(in.GetDefaultValue())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid boolean default value: %w", err))
		}
		defaultValue = parsed
	}
	err := h.FeatureFlagRepo.UpsertFlag(ctx, domain.FeatureFlag{
		Key:          in.GetKey(),
		Name:         in.GetName(),
		Type:         flagType,
		Enabled:      in.GetEnabled(),
		DefaultValue: defaultValue,
		Description:  in.GetDescription(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pb.UpsertFeatureFlagResponse{Message: "Flag updated successfully"}), nil
}

func (h *ConfigHandler) ListInputs(
	ctx context.Context,
	req *connect.Request[pb.ListInputsRequest],
) (*connect.Response[pb.ListInputsResponse], error) {
	span := trace.SpanFromContext(ctx)

	output, err := h.ListInputsUC.Execute(ctx, videoapp.ListInputsUseCaseInput{})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to list input sources")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbSources := make([]*pb.InputSource, len(output.Inputs))
	for i, src := range output.Inputs {
		pbSources[i] = &pb.InputSource{
			Id:               src.ID,
			DisplayName:      src.DisplayName,
			Type:             src.Type,
			ImagePath:        src.ImagePath,
			IsDefault:        src.IsDefault,
			VideoPath:        src.VideoPath,
			PreviewImagePath: src.PreviewImagePath,
		}
	}

	span.SetAttributes(
		attribute.Int("input_sources.count", len(pbSources)),
	)

	return connect.NewResponse(&pb.ListInputsResponse{
		Sources: pbSources,
	}), nil
}

func (h *ConfigHandler) GetAvailableTools(
	ctx context.Context,
	req *connect.Request[pb.GetAvailableToolsRequest],
) (*connect.Response[pb.GetAvailableToolsResponse], error) {
	span := trace.SpanFromContext(ctx)

	if h.ConfigManager == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("config manager not available"))
	}

	categories := []*pb.ToolCategory{}

	if len(h.ConfigManager.Tools.Observability) > 0 {
		tools := h.buildTools(h.ConfigManager.Tools.Observability)
		categories = append(categories, &pb.ToolCategory{
			Id:    "observability",
			Name:  "Observability",
			Tools: tools,
		})
	}

	if len(h.ConfigManager.Tools.Features) > 0 {
		tools := h.buildTools(h.ConfigManager.Tools.Features)
		categories = append(categories, &pb.ToolCategory{
			Id:    "features",
			Name:  "Features",
			Tools: tools,
		})
	}

	if len(h.ConfigManager.Tools.Testing) > 0 {
		tools := h.buildTools(h.ConfigManager.Tools.Testing)
		categories = append(categories, &pb.ToolCategory{
			Id:    "testing",
			Name:  "Testing",
			Tools: tools,
		})
	}

	span.SetAttributes(
		attribute.String("config.environment", h.ConfigManager.Environment),
		attribute.Int("tools.category_count", len(categories)),
	)

	logger.FromContext(ctx).Debug().
		Int("category_count", len(categories)).
		Str("environment", h.ConfigManager.Environment).
		Msg("GetAvailableTools: returning categories")

	return connect.NewResponse(&pb.GetAvailableToolsResponse{
		Categories: categories,
	}), nil
}

func (h *ConfigHandler) buildTools(toolDefs []config.ToolDefinition) []*pb.Tool {
	tools := make([]*pb.Tool, 0, len(toolDefs))

	for _, toolDef := range toolDefs {
		tool := &pb.Tool{
			Id:       toolDef.ID,
			Name:     toolDef.Name,
			IconPath: toolDef.IconPath,
			Type:     toolDef.Type,
		}

		if toolDef.Type == "url" {
			tool.Url = toolDef.URL
		} else if toolDef.Type == "action" {
			tool.Action = toolDef.Action
		}

		tools = append(tools, tool)
	}

	return tools
}

func (h *ConfigHandler) GetSystemInfo(
	ctx context.Context,
	req *connect.Request[pb.GetSystemInfoRequest],
) (*connect.Response[pb.GetSystemInfoResponse], error) {
	span := trace.SpanFromContext(ctx)

	output, err := h.GetSystemInfoUC.Execute(ctx, systemapp.GetSystemInfoUseCaseInput{})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to get system info")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	systemInfo := output.SystemInfo

	// Map domain to proto
	response := &pb.GetSystemInfoResponse{
		Version: &pb.SystemVersion{
			GoVersion:    systemInfo.Version.GoVersion,
			CppVersion:   systemInfo.Version.CppVersion,
			ProtoVersion: systemInfo.Version.ProtoVersion,
			Branch:       systemInfo.Version.Branch,
			BuildTime:    systemInfo.Version.BuildTime,
			CommitHash:   systemInfo.Version.CommitHash,
		},
		Environment: systemInfo.Environment,
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("system.version.go", systemInfo.Version.GoVersion),
		attribute.String("system.version.cpp", systemInfo.Version.CppVersion),
		attribute.String("system.version.proto", systemInfo.Version.ProtoVersion),
		attribute.String("system.version.branch", systemInfo.Version.Branch),
		attribute.String("system.version.build_time", systemInfo.Version.BuildTime),
		attribute.String("system.version.commit_hash", systemInfo.Version.CommitHash),
		attribute.String("system.environment", systemInfo.Environment),
	)

	logger.FromContext(ctx).Debug().
		Str("environment", systemInfo.Environment).
		Str("go_version", systemInfo.Version.GoVersion).
		Msg("GetSystemInfo: returning system info")

	return connect.NewResponse(response), nil
}

// GetProcessorStatus is intentionally a no-op now: filter capabilities and
// version information flow directly from the C++ accelerator to the browser
// over the WebRTC "control" data channel. We keep the RPC so the existing
// frontend client doesn't break, but it returns an empty payload.
func (h *ConfigHandler) GetProcessorStatus(
	ctx context.Context,
	req *connect.Request[pb.GetProcessorStatusRequest],
) (*connect.Response[pb.GetProcessorStatusResponse], error) {
	_ = ctx
	_ = req
	return connect.NewResponse(&pb.GetProcessorStatusResponse{}), nil
}
