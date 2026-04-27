package connectrpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	"github.com/jrb/cuda-learning/src/go_api/pkg/interfaces/adapters"
)

// VideoPlaybackHandler serves the VideoPlaybackService Connect RPCs. It owns
// the start/stop lifecycle for streaming a previously uploaded video into a
// running WebRTC session — the only image-processing concern still routed
// through Go (filters and version queries now run over the WebRTC control
// data channel directly between the browser and the C++ accelerator).
type VideoPlaybackHandler struct {
	startVideoPlaybackUC useCase[videoapp.StartVideoPlaybackUseCaseInput, videoapp.StartVideoPlaybackUseCaseOutput]
	stopVideoPlaybackUC  useCase[videoapp.StopVideoPlaybackUseCaseInput, videoapp.StopVideoPlaybackUseCaseOutput]
	adapter              *adapters.ProtobufAdapter
}

func NewVideoPlaybackHandler(
	startVideoPlaybackUC useCase[videoapp.StartVideoPlaybackUseCaseInput, videoapp.StartVideoPlaybackUseCaseOutput],
	stopVideoPlaybackUC useCase[videoapp.StopVideoPlaybackUseCaseInput, videoapp.StopVideoPlaybackUseCaseOutput],
) *VideoPlaybackHandler {
	return &VideoPlaybackHandler{
		startVideoPlaybackUC: startVideoPlaybackUC,
		stopVideoPlaybackUC:  stopVideoPlaybackUC,
		adapter:              adapters.NewProtobufAdapter(),
	}
}

func (h *VideoPlaybackHandler) StartVideoPlayback(
	ctx context.Context,
	req *connect.Request[pb.StartVideoPlaybackRequest],
) (*connect.Response[pb.StartVideoPlaybackResponse], error) {
	if h.startVideoPlaybackUC == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("start video playback use case not configured"))
	}

	input := videoapp.StartVideoPlaybackUseCaseInput{
		VideoID:        req.Msg.GetVideoId(),
		SessionID:      req.Msg.GetSessionId(),
		Filters:        h.adapter.ToFilters(req.Msg.GetFilters()),
		Accelerator:    h.adapter.ToAccelerator(req.Msg.GetAccelerator()),
		GrayscaleType:  h.adapter.ToGrayscaleType(req.Msg.GetGrayscaleType()),
		BlurParams:     h.adapter.ToBlurParameters(req.Msg.GetBlurParams()),
		GenericFilters: req.Msg.GetGenericFilters(),
		ModelParams:    req.Msg.GetModelParams(),
		TraceContext:   req.Msg.GetTraceContext().GetTraceparent(),
		APIVersion:     req.Msg.GetApiVersion(),
	}

	result, err := h.startVideoPlaybackUC.Execute(ctx, input)
	if err != nil {
		return nil, connect.NewError(mapStreamVideoError(err), err)
	}

	resp := &pb.StartVideoPlaybackResponse{
		Code:         result.Code,
		Message:      result.Message,
		SessionId:    result.SessionID,
		TraceContext: &pb.TraceContext{Traceparent: result.TraceContext},
		ApiVersion:   result.APIVersion,
	}
	return connect.NewResponse(resp), nil
}

func (h *VideoPlaybackHandler) StopVideoPlayback(
	ctx context.Context,
	req *connect.Request[pb.StopVideoPlaybackRequest],
) (*connect.Response[pb.StopVideoPlaybackResponse], error) {
	if h.stopVideoPlaybackUC == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("stop video playback use case not configured"))
	}

	input := videoapp.StopVideoPlaybackUseCaseInput{
		SessionID:    req.Msg.GetSessionId(),
		TraceContext: req.Msg.GetTraceContext().GetTraceparent(),
		APIVersion:   req.Msg.GetApiVersion(),
	}

	result, err := h.stopVideoPlaybackUC.Execute(ctx, input)
	if err != nil {
		return nil, connect.NewError(mapStreamVideoError(err), err)
	}

	resp := &pb.StopVideoPlaybackResponse{
		Code:         result.Code,
		Message:      result.Message,
		SessionId:    result.SessionID,
		Stopped:      result.Stopped,
		TraceContext: &pb.TraceContext{Traceparent: result.TraceContext},
		ApiVersion:   result.APIVersion,
	}
	return connect.NewResponse(resp), nil
}

func mapStreamVideoError(err error) connect.Code {
	switch {
	case errors.Is(err, videoapp.ErrVideoPlaybackMissingVideoID),
		errors.Is(err, videoapp.ErrVideoPlaybackMissingSession):
		return connect.CodeInvalidArgument
	case errors.Is(err, videoapp.ErrVideoPlaybackAlreadyRunning):
		return connect.CodeAlreadyExists
	case errors.Is(err, videoapp.ErrVideoPlaybackNotRunning):
		return connect.CodeNotFound
	default:
		return connect.CodeInternal
	}
}

var _ genconnect.VideoPlaybackServiceHandler = (*VideoPlaybackHandler)(nil)
