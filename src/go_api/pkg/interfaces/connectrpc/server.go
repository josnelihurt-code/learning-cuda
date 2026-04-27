package connectrpc

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	imageapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/image"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
)

func RegisterVideoPlaybackService(mux *http.ServeMux, handler *VideoPlaybackHandler, interceptors ...connect.Interceptor) {
	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewVideoPlaybackServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}

func RegisterConfigService(
	mux *http.ServeMux,
	deps ConfigHandlerDeps,
	interceptors ...connect.Interceptor,
) {
	configHandler := NewConfigHandler(deps)

	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewConfigServiceHandler(configHandler, opts...)
	mux.Handle(path, rpcHandler)
}

func RegisterFileService(
	mux *http.ServeMux,
	listAvailableImagesUC useCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput],
	uploadImageUC useCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput],
	listVideosUC useCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput],
	uploadVideoUC useCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput],
	interceptors ...connect.Interceptor,
) {
	fileHandler := NewFileHandler(listAvailableImagesUC, uploadImageUC, listVideosUC, uploadVideoUC)

	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewFileServiceHandler(fileHandler, opts...)
	mux.Handle(path, rpcHandler)
}

func RegisterWebRTCSignalingService(
	mux *http.ServeMux,
	client WebRTCSignalingClient,
	interceptors ...connect.Interceptor,
) {
	handler := NewWebRTCSignalingHandler(client)

	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewWebRTCSignalingServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}

func RegisterRemoteManagementService(
	mux *http.ServeMux,
	gateway acceleratorGateway,
	configManager *config.Manager,
	dm deviceMonitor,
	interceptors ...connect.Interceptor,
) {
	handler := NewRemoteManagementHandler(gateway, configManager, dm)

	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewRemoteManagementServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}
