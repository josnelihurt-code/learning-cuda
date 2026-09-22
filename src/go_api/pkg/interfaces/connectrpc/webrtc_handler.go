package connectrpc

import (
	"context"
	"errors"
	"io"
	"sync"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

type webRTCSignalingClient interface {
	SignalingStream(ctx context.Context) (pb.WebRTCSignalingService_SignalingStreamClient, error)
}

type webRTCSignalingHandler struct {
	client  webRTCSignalingClient
	manager *webRTCSignalingSessionManager
}

func NewWebRTCSignalingHandler(client webRTCSignalingClient) *webRTCSignalingHandler {
	return &webRTCSignalingHandler{
		client:  client,
		manager: newWebRTCSignalingSessionManager(client),
	}
}

func (h *webRTCSignalingHandler) StartSession(
	ctx context.Context,
	req *connect.Request[pb.StartSessionRequest],
) (*connect.Response[pb.StartSessionResponse], error) {
	resp, err := h.manager.StartSession(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(resp), nil
}

func (h *webRTCSignalingHandler) SendIceCandidate(
	ctx context.Context,
	req *connect.Request[pb.SendIceCandidateRequest],
) (*connect.Response[pb.SendIceCandidateResponse], error) {
	resp, err := h.manager.SendIceCandidate(ctx, req.Msg)
	if err != nil {
		if errors.Is(err, errSignalingSessionNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(resp), nil
}

func (h *webRTCSignalingHandler) PollEvents(
	ctx context.Context,
	req *connect.Request[pb.PollEventsRequest],
) (*connect.Response[pb.PollEventsResponse], error) {
	resp, err := h.manager.PollEvents(ctx, req.Msg)
	if err != nil {
		switch {
		case errors.Is(err, errSignalingSessionNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, errSignalingSessionClosed):
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	return connect.NewResponse(resp), nil
}

func (h *webRTCSignalingHandler) CloseSession(
	ctx context.Context,
	req *connect.Request[pb.CloseSessionRequest],
) (*connect.Response[pb.CloseSessionResponse], error) {
	resp, err := h.manager.CloseSession(ctx, req.Msg)
	if err != nil {
		if errors.Is(err, errSignalingSessionNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(resp), nil
}

func getMessageTypeString(msg *pb.SignalingMessage) string {
	switch {
	case msg.GetStartSession() != nil:
		return "start_session"
	case msg.GetStartSessionResponse() != nil:
		return "start_session_response"
	case msg.GetIceCandidate() != nil:
		return "ice_candidate"
	case msg.GetIceCandidateResponse() != nil:
		return "ice_candidate_response"
	case msg.GetCloseSession() != nil:
		return "close_session"
	case msg.GetCloseSessionResponse() != nil:
		return "close_session_response"
	case msg.GetKeepAlive() != nil:
		return "keep_alive"
	default:
		return "unknown"
	}
}

// SignalingStream implements bidirectional streaming proxy between frontend and C++ gRPC server.
// It forwards messages in both directions:
// - Frontend → C++: Via goroutine that reads from Connect-RPC stream and sends to gRPC stream
// - C++ → Frontend: Via main loop that reads from gRPC stream and sends to Connect-RPC stream
// Uses context cancellation and WaitGroups for proper cleanup of goroutines.
func (h *webRTCSignalingHandler) SignalingStream(
	ctx context.Context,
	stream *connect.BidiStream[pb.SignalingMessage, pb.SignalingMessage],
) error {
	logger.Global().Info().Msg("WebRTC SignalingStream started")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	grpcStream, err := h.client.SignalingStream(ctx)
	if err != nil {
		logger.Global().Error().Err(err).Msg("Failed to create gRPC stream to C++ server")
		return connect.NewError(connect.CodeInternal, err)
	}
	defer func() {
		if err := grpcStream.CloseSend(); err != nil {
			logger.Global().Warn().Err(err).Msg("Failed to close gRPC stream")
		}
	}()

	var wg sync.WaitGroup
	var frontendErr error
	var grpcErr error

	wg.Add(1)
	go h.forwardFrontendToGRPC(stream, grpcStream, cancel, &frontendErr, &wg)

	for {
		msg, err := grpcStream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Global().Info().Msg("gRPC stream closed by C++ server")
				break
			}
			logger.Global().Error().
				Err(err).
				Msg("Failed to receive message from C++ server")
			grpcErr = err
			cancel()
			break
		}

		messageType := getMessageTypeString(msg)
		logger.Global().Debug().
			Str("message_type", messageType).
			Msg("Forwarding message from C++ to frontend")

		if err := stream.Send(msg); err != nil {
			logger.Global().Error().
				Err(err).
				Msg("Failed to send message to frontend")
			grpcErr = err
			cancel()
			break
		}
	}

	wg.Wait()

	if frontendErr != nil {
		return connect.NewError(connect.CodeInternal, frontendErr)
	}
	if grpcErr != nil {
		return connect.NewError(connect.CodeInternal, grpcErr)
	}

	return nil
}

func (h *webRTCSignalingHandler) forwardFrontendToGRPC(
	stream *connect.BidiStream[pb.SignalingMessage, pb.SignalingMessage],
	grpcStream pb.WebRTCSignalingService_SignalingStreamClient,
	cancel context.CancelFunc,
	//nolint:gocritic // ptrToRefParam: pointer needed to set error from goroutine
	frontendErr *error,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	for {
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Global().Info().Msg("Frontend stream closed")
				return
			}
			logger.Global().Error().
				Err(err).
				Msg("Error receiving from frontend stream")
			*frontendErr = err
			cancel()
			return
		}

		logger.Global().Debug().
			Msg("Forwarding message from frontend to C++")

		if err := grpcStream.Send(msg); err != nil {
			logger.Global().Error().
				Err(err).
				Msg("Failed to forward message to C++ server")
			*frontendErr = err
			cancel()
			return
		}
	}
}

var _ genconnect.WebRTCSignalingServiceHandler = (*webRTCSignalingHandler)(nil)
