package connectrpc

import (
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/vanguard"
	"github.com/jrb/cuda-learning/proto/gen/genconnect"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

// SetupVanguardTranscoder builds the /api/ transcoder (REST + Connect + gRPC)
// from the same handler instances that are registered on the mux, so each
// handler is constructed exactly once per process.
func SetupVanguardTranscoder(
	configHandler *ConfigHandler,
	fileHandler *FileHandler,
	interceptors []connect.Interceptor,
) http.Handler {
	log := logger.Global()

	var opts []connect.HandlerOption
	if len(interceptors) > 0 {
		opts = append(opts, connect.WithInterceptors(interceptors...))
	}

	_, configConnectHandler := genconnect.NewConfigServiceHandler(configHandler, opts...)
	_, fileConnectHandler := genconnect.NewFileServiceHandler(fileHandler, opts...)

	services := []*vanguard.Service{
		vanguard.NewService(genconnect.ConfigServiceName, configConnectHandler),
		vanguard.NewService(genconnect.FileServiceName, fileConnectHandler),
	}

	transcoder, err := vanguard.NewTranscoder(services)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to create Vanguard transcoder")
		panic("failed to create vanguard transcoder: " + err.Error())
	}

	return transcoder
}
