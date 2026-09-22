package connectrpc

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
)

// RegisterConfigService registers an already-constructed configHandler on mux.
// Handlers are built once by the composition root and shared with the
// Vanguard transcoder (see SetupVanguardTranscoder).
func RegisterConfigService(
	mux *http.ServeMux,
	handler *configHandler,
	interceptors ...connect.Interceptor,
) {
	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewConfigServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}

// RegisterFileService registers an already-constructed fileHandler on mux.
func RegisterFileService(
	mux *http.ServeMux,
	handler *fileHandler,
	interceptors ...connect.Interceptor,
) {
	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewFileServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}

// RegisterWebRTCSignalingService registers an already-constructed
// webRTCSignalingHandler on mux.
func RegisterWebRTCSignalingService(
	mux *http.ServeMux,
	handler *webRTCSignalingHandler,
	interceptors ...connect.Interceptor,
) {
	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewWebRTCSignalingServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}

// RegisterRemoteManagementService registers an already-constructed
// remoteManagementHandler on mux.
func RegisterRemoteManagementService(
	mux *http.ServeMux,
	handler *remoteManagementHandler,
	interceptors ...connect.Interceptor,
) {
	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	path, rpcHandler := genconnect.NewRemoteManagementServiceHandler(handler, opts...)
	mux.Handle(path, rpcHandler)
}
