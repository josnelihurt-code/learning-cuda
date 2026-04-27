package connectrpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)


type acceleratorGateway interface {
	IsAvailable() bool
}

type deviceMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	PowerOn() error
	Subscribe(callback func(status *domain.DeviceStatus)) func()
}

type RemoteManagementHandler struct {
	gateway       acceleratorGateway
	config        *config.Manager
	deviceMonitor deviceMonitor
}

func NewRemoteManagementHandler(gateway acceleratorGateway, configManager *config.Manager, dm deviceMonitor) *RemoteManagementHandler {
	return &RemoteManagementHandler{
		gateway:       gateway,
		config:        configManager,
		deviceMonitor: dm,
	}
}

func (h *RemoteManagementHandler) StartJetsonNano(
	ctx context.Context,
	req *connect.Request[pb.StartJetsonNanoRequest],
) (*connect.Response[pb.StartJetsonNanoResponse], error) {
	span := trace.SpanFromContext(ctx)

	if h.deviceMonitor == nil {
		span.RecordError(fmt.Errorf("device monitor not available"))
		return nil, connect.NewError(connect.CodeInternal, errors.New("device monitor not initialized"))
	}

	if err := h.deviceMonitor.PowerOn(); err != nil {
		span.RecordError(err)
		return connect.NewResponse(&pb.StartJetsonNanoResponse{
			Status:       pb.StartJetsonNanoStatus_START_JETSON_NANO_STATUS_ERROR,
			Step:         "error",
			Message:      fmt.Sprintf("Failed to send power command: %v", err),
			TraceContext: req.Msg.TraceContext,
		}), nil
	}

	span.SetAttributes(
		attribute.Bool("jetson.power_command_sent", true),
	)

	return connect.NewResponse(&pb.StartJetsonNanoResponse{
		Status:       pb.StartJetsonNanoStatus_START_JETSON_NANO_STATUS_SUCCESS,
		Step:         "sent",
		Message:      "POWER ON command sent successfully",
		TraceContext: req.Msg.TraceContext,
	}), nil
}

type healthCheckResult struct {
	status         pb.AcceleratorHealthStatus
	message        string
	serverVersion  string
	libraryVersion string
}

func (h *RemoteManagementHandler) checkAcceleratorHealth(ctx context.Context) healthCheckResult {
	span := trace.SpanFromContext(ctx)

	if h.gateway == nil || !h.gateway.IsAvailable() {
		span.SetAttributes(attribute.Bool("accelerator.healthy", false))
		return healthCheckResult{
			status:  pb.AcceleratorHealthStatus_ACCELERATOR_HEALTH_STATUS_UNHEALTHY,
			message: "no accelerator registered",
		}
	}

	span.SetAttributes(attribute.Bool("accelerator.healthy", true))
	return healthCheckResult{
		status:  pb.AcceleratorHealthStatus_ACCELERATOR_HEALTH_STATUS_HEALTHY,
		message: "Accelerator is healthy",
	}
}

func (h *RemoteManagementHandler) CheckAcceleratorHealth(
	ctx context.Context,
	req *connect.Request[pb.CheckAcceleratorHealthRequest],
) (*connect.Response[pb.CheckAcceleratorHealthResponse], error) {
	result := h.checkAcceleratorHealth(ctx)

	return connect.NewResponse(&pb.CheckAcceleratorHealthResponse{
		Status:         result.status,
		Message:        result.message,
		ServerVersion:  result.serverVersion,
		LibraryVersion: result.libraryVersion,
		TraceContext:   req.Msg.TraceContext,
	}), nil
}

func (h *RemoteManagementHandler) MonitorJetsonNano(
	ctx context.Context,
	req *connect.Request[pb.MonitorJetsonNanoRequest],
	stream *connect.ServerStream[pb.MonitorJetsonNanoResponse],
) error {
	span := trace.SpanFromContext(ctx)

	if h.deviceMonitor == nil {
		span.RecordError(fmt.Errorf("device monitor not available"))
		return connect.NewError(connect.CodeInternal, errors.New("device monitor not initialized"))
	}

	initialMsg := "Connected to MQTT device monitor. Sending last known status and recent messages..."
	if err := stream.Send(&pb.MonitorJetsonNanoResponse{
		Data: initialMsg,
	}); err != nil {
		span.RecordError(err)
		return err
	}

	updateChan := make(chan *domain.DeviceStatus, 10)
	healthChan := make(chan healthCheckResult, 10)

	unsubscribe := h.deviceMonitor.Subscribe(func(status *domain.DeviceStatus) {
		select {
		case updateChan <- status:
		default:
		}
	})
	defer unsubscribe()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result := h.checkAcceleratorHealth(ctx)
				select {
				case healthChan <- result:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status := <-updateChan:
			if err := stream.Send(&pb.MonitorJetsonNanoResponse{
				Data: status.String(),
			}); err != nil {
				span.RecordError(err)
				return err
			}
		case health := <-healthChan:
			healthMsg := fmt.Sprintf("Accelerator Health: %s - %s", health.status.String(), health.message)
			if health.serverVersion != "" {
				healthMsg += fmt.Sprintf(" (Server: %s, Library: %s)", health.serverVersion, health.libraryVersion)
			}
			if err := stream.Send(&pb.MonitorJetsonNanoResponse{
				Data: healthMsg,
			}); err != nil {
				span.RecordError(err)
				return err
			}
		}
	}
}

var _ genconnect.RemoteManagementServiceHandler = (*RemoteManagementHandler)(nil)
