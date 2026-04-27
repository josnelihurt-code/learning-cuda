package processor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	gen "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// ControlServer hosts AcceleratorControlServiceServer. v1 = single accelerator,
// but the type holds room for the registry that step-05 will introduce.
type ControlServer struct {
	gen.UnimplementedAcceleratorControlServiceServer
	cfg      config.ProcessorConfig
	log      zerolog.Logger
	grpcSrv  *grpc.Server
	lis      net.Listener
	registry *Registry
}

func NewControlServer(cfg config.ProcessorConfig, registry *Registry) (*ControlServer, error) {
	if cfg.ListenAddress == "" {
		return nil, fmt.Errorf("processor.listen_address is required")
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(cfg.TLS.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA file: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid certificates found in client CA file %s", cfg.TLS.ClientCAFile)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}

	creds := credentials.NewTLS(tlsCfg)
	grpcSrv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(64<<20),
		grpc.MaxSendMsgSize(64<<20),
	)

	srv := &ControlServer{
		cfg:      cfg,
		log:      *logger.Global(),
		grpcSrv:  grpcSrv,
		registry: registry,
	}
	gen.RegisterAcceleratorControlServiceServer(grpcSrv, srv)
	return srv, nil
}

func (s *ControlServer) Start() error {
	lis, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("control server listen on %s: %w", s.cfg.ListenAddress, err)
	}
	s.lis = lis
	s.log.Info().Str("address", s.cfg.ListenAddress).Msg("control server listening")

	go func() {
		if err := s.grpcSrv.Serve(lis); err != nil {
			s.log.Error().Err(err).Msg("control server stopped unexpectedly")
		}
	}()
	return nil
}

func (s *ControlServer) Stop(ctx context.Context) {
	stopped := make(chan struct{})
	go func() {
		s.grpcSrv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		s.log.Info().Msg("control server stopped")
	case <-ctx.Done():
		s.grpcSrv.Stop()
		s.log.Warn().Msg("control server force-stopped after deadline")
	}
}

// Connect implements AcceleratorControlServiceServer.
func (s *ControlServer) Connect(stream grpc.BidiStreamingServer[gen.ConnectRequest, gen.ConnectResponse]) error {
	// 1. Receive first message; expect a Register payload.
	first, err := stream.Recv()
	if err != nil {
		return err
	}

	reg, ok := first.Message.GetPayload().(*gen.AcceleratorMessage_Register)
	if !ok {
		ack := &gen.ConnectResponse{Message: &gen.AcceleratorMessage{
			CommandId: first.Message.GetCommandId(),
			Payload: &gen.AcceleratorMessage_RegisterAck{
				RegisterAck: &gen.RegisterAck{
					Accepted:     false,
					RejectReason: "first message must be Register",
				},
			},
		}}
		_ = stream.Send(ack)
		return status.Error(codes.InvalidArgument, "first message must be Register")
	}

	// 2. Construct session; let the registry enforce the v1 single-accelerator policy.
	assignedID := uuid.NewString()
	sess := newAcceleratorSession(assignedID, reg.Register, stream, *logger.Global())

	if err := s.registry.Add(sess); err != nil {
		ack := &gen.ConnectResponse{Message: &gen.AcceleratorMessage{
			CommandId: first.Message.GetCommandId(),
			Payload: &gen.AcceleratorMessage_RegisterAck{
				RegisterAck: &gen.RegisterAck{
					Accepted:     false,
					RejectReason: err.Error(),
				},
			},
		}}
		_ = stream.Send(ack)
		return status.Error(codes.AlreadyExists, err.Error())
	}

	s.log.Info().
		Str("device_id", reg.Register.DeviceId).
		Str("display_name", reg.Register.DisplayName).
		Str("version", reg.Register.AcceleratorVersion).
		Str("assigned_session_id", assignedID).
		Msg("accelerator connected")

	defer func() {
		s.registry.Remove(sess.DeviceID)
		sess.cancel()
		sess.pending.cancelAll()
		sess.closeAllSignaling()
		s.log.Info().
			Str("device_id", sess.DeviceID).
			Msg("accelerator disconnected")
	}()

	// 3. Acknowledge registration.
	ack := &gen.ConnectResponse{Message: &gen.AcceleratorMessage{
		CommandId: first.Message.GetCommandId(),
		Payload: &gen.AcceleratorMessage_RegisterAck{
			RegisterAck: &gen.RegisterAck{
				Accepted:          true,
				AssignedSessionId: assignedID,
			},
		},
	}}
	if err := stream.Send(ack); err != nil {
		return err
	}

	// 4. Receive loop.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		env := msg.Message
		switch env.GetPayload().(type) {
		case *gen.AcceleratorMessage_Keepalive:
			sess.lastSeen = time.Now()
		case *gen.AcceleratorMessage_Register:
			return status.Error(codes.FailedPrecondition, "re-register on existing stream")
		case *gen.AcceleratorMessage_SignalingMessage:
			sess.deliverSignaling(env)
		case *gen.AcceleratorMessage_Error:
			if !sess.pending.deliver(env.GetCommandId(), env) {
				s.log.Warn().
					Str("command_id", env.GetCommandId()).
					Msg("response without matching pending command")
			}
		default:
			s.log.Debug().Msg("unexpected message type from accelerator")
		}
	}
}
