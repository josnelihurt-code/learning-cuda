package processor

import (
	"context"
	"io"

	"github.com/google/uuid"
	gen "github.com/jrb/cuda-learning/proto/gen"
	"google.golang.org/grpc/metadata"
)

// signalingStreamAdapter implements grpc.BidiStreamingClient[SignalingMessage, SignalingMessage]
// (aliased as gen.WebRTCSignalingService_SignalingStreamClient) by routing through the
// registered accelerator's bidi control stream.
type signalingStreamAdapter struct {
	sess   *acceleratorSession
	subID  string
	inCh   <-chan *gen.AcceleratorMessage
	unsub  func()
	ctx    context.Context
	cancel context.CancelFunc
}

func newSignalingStreamAdapter(ctx context.Context, sess *acceleratorSession) *signalingStreamAdapter {
	adapterCtx, cancel := context.WithCancel(ctx)
	subID := uuid.NewString()
	inCh, unsub := sess.SubscribeSignaling(subID)
	return &signalingStreamAdapter{
		sess:   sess,
		subID:  subID,
		inCh:   inCh,
		unsub:  unsub,
		ctx:    adapterCtx,
		cancel: cancel,
	}
}

// Send wraps msg in an AcceleratorMessage_SignalingMessage and sends it to the accelerator.
func (a *signalingStreamAdapter) Send(msg *gen.SignalingMessage) error {
	return a.sess.Send(&gen.AcceleratorMessage{
		CommandId: uuid.NewString(),
		Payload: &gen.AcceleratorMessage_SignalingMessage{
			SignalingMessage: msg,
		},
	})
}

// Recv returns the next SignalingMessage from the accelerator, blocking until one arrives.
func (a *signalingStreamAdapter) Recv() (*gen.SignalingMessage, error) {
	select {
	case env, ok := <-a.inCh:
		if !ok {
			return nil, io.EOF
		}
		sig := env.GetSignalingMessage()
		if sig == nil {
			return nil, io.EOF
		}
		return sig, nil
	case <-a.ctx.Done():
		return nil, a.ctx.Err()
	case <-a.sess.ctx.Done():
		return nil, io.EOF
	}
}

// CloseSend unsubscribes from signaling fanout and cancels the adapter context.
func (a *signalingStreamAdapter) CloseSend() error {
	a.cancel()
	a.unsub()
	return nil
}

// Context returns the adapter's context.
func (a *signalingStreamAdapter) Context() context.Context {
	return a.ctx
}

// Header, Trailer, SendMsg, RecvMsg are stubs required by grpc.ClientStream.
func (a *signalingStreamAdapter) Header() (metadata.MD, error) { return nil, nil }
func (a *signalingStreamAdapter) Trailer() metadata.MD         { return nil }
func (a *signalingStreamAdapter) SendMsg(_ any) error          { return nil }
func (a *signalingStreamAdapter) RecvMsg(_ any) error          { return nil }
