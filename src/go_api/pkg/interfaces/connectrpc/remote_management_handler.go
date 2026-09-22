package connectrpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	"github.com/jrb/cuda-learning/src/go_api/pkg/application"
	remoteapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/remote"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type deviceStatusSource interface {
	Subscribe(callback func(status *domain.DeviceStatus)) func()
}

type remoteManagementHandler struct {
	startUC      application.UseCase[remoteapp.StartJetsonNanoUseCaseInput, remoteapp.StartJetsonNanoUseCaseOutput]
	healthUC     application.UseCase[remoteapp.CheckAcceleratorHealthUseCaseInput, remoteapp.CheckAcceleratorHealthUseCaseOutput]
	statusSource deviceStatusSource
}

func NewRemoteManagementHandler(
	startUC application.UseCase[remoteapp.StartJetsonNanoUseCaseInput, remoteapp.StartJetsonNanoUseCaseOutput],
	healthUC application.UseCase[remoteapp.CheckAcceleratorHealthUseCaseInput, remoteapp.CheckAcceleratorHealthUseCaseOutput],
	statusSource deviceStatusSource,
) *remoteManagementHandler {
	return &remoteManagementHandler{
		startUC:      startUC,
		healthUC:     healthUC,
		statusSource: statusSource,
	}
}

func (h *remoteManagementHandler) StartJetsonNano(
	ctx context.Context,
	req *connect.Request[pb.StartJetsonNanoRequest],
) (*connect.Response[pb.StartJetsonNanoResponse], error) {
	span := trace.SpanFromContext(ctx)

	out, err := h.startUC.Execute(ctx, remoteapp.StartJetsonNanoUseCaseInput{})
	if err != nil {
		span.RecordError(err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	status := pb.StartJetsonNanoStatus_START_JETSON_NANO_STATUS_SUCCESS
	if !out.Success {
		status = pb.StartJetsonNanoStatus_START_JETSON_NANO_STATUS_ERROR
	} else {
		span.SetAttributes(attribute.Bool("jetson.power_command_sent", true))
	}

	return connect.NewResponse(&pb.StartJetsonNanoResponse{
		Status:       status,
		Step:         out.Step,
		Message:      out.Message,
		TraceContext: req.Msg.TraceContext,
	}), nil
}

func (h *remoteManagementHandler) mapHealth(out remoteapp.CheckAcceleratorHealthUseCaseOutput) (pb.AcceleratorHealthStatus, string) {
	if out.Healthy {
		return pb.AcceleratorHealthStatus_ACCELERATOR_HEALTH_STATUS_HEALTHY, out.Message
	}
	return pb.AcceleratorHealthStatus_ACCELERATOR_HEALTH_STATUS_UNHEALTHY, out.Message
}

func (h *remoteManagementHandler) CheckAcceleratorHealth(
	ctx context.Context,
	req *connect.Request[pb.CheckAcceleratorHealthRequest],
) (*connect.Response[pb.CheckAcceleratorHealthResponse], error) {
	out, err := h.healthUC.Execute(ctx, remoteapp.CheckAcceleratorHealthUseCaseInput{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	status, message := h.mapHealth(out)

	return connect.NewResponse(&pb.CheckAcceleratorHealthResponse{
		Status:       status,
		Message:      message,
		TraceContext: req.Msg.TraceContext,
	}), nil
}

func (h *remoteManagementHandler) MonitorJetsonNano(
	ctx context.Context,
	req *connect.Request[pb.MonitorJetsonNanoRequest],
	stream *connect.ServerStream[pb.MonitorJetsonNanoResponse],
) error {
	_ = req
	span := trace.SpanFromContext(ctx)

	if h.statusSource == nil {
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
	healthChan := make(chan remoteapp.CheckAcceleratorHealthUseCaseOutput, 10)

	unsubscribe := h.statusSource.Subscribe(func(status *domain.DeviceStatus) {
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
				out, err := h.healthUC.Execute(ctx, remoteapp.CheckAcceleratorHealthUseCaseInput{})
				if err != nil {
					continue
				}
				select {
				case healthChan <- out:
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
			pbStatus, message := h.mapHealth(health)
			healthMsg := fmt.Sprintf("Accelerator Health: %s - %s", pbStatus.String(), message)
			if err := stream.Send(&pb.MonitorJetsonNanoResponse{
				Data: healthMsg,
			}); err != nil {
				span.RecordError(err)
				return err
			}
		}
	}
}

var _ genconnect.RemoteManagementServiceHandler = (*remoteManagementHandler)(nil)
