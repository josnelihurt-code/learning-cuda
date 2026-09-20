package processor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	gen "github.com/jrb/cuda-learning/proto/gen"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

// AcceleratorSession represents a live connection from one registered accelerator.
type AcceleratorSession struct {
	DeviceID        string
	DisplayName     string
	AssignedSession string // server-minted UUID; matches RegisterAck.assigned_session_id
	Capabilities    *gen.LibraryCapabilities
	SupportedTypes  []gen.AcceleratorType
	Cameras         []*gen.RemoteCameraInfo

	stream            grpc.BidiStreamingServer[gen.ConnectRequest, gen.ConnectResponse]
	sendMu            sync.Mutex // gRPC bidi requires single-writer; serialize sends.
	lastSeenMu        sync.RWMutex
	lastSeen          time.Time
	keepaliveInterval time.Duration
	keepaliveTimeout  time.Duration

	pending      *pendingMap
	signalingMu  sync.RWMutex
	signalingChs map[string]chan *gen.AcceleratorMessage // key: subscriber id

	ctx    context.Context
	cancel context.CancelFunc
	log    zerolog.Logger
}

func newAcceleratorSession(
	assignedID string,
	reg *gen.Register,
	stream grpc.BidiStreamingServer[gen.ConnectRequest, gen.ConnectResponse],
	keepaliveInterval time.Duration,
	keepaliveTimeout time.Duration,
	log zerolog.Logger,
) *AcceleratorSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &AcceleratorSession{
		DeviceID:          reg.DeviceId,
		DisplayName:       reg.DisplayName,
		AssignedSession:   assignedID,
		Capabilities:      reg.Capabilities,
		SupportedTypes:    reg.SupportedAcceleratorTypes,
		Cameras:           reg.Cameras,
		stream:            stream,
		lastSeen:          time.Now(),
		keepaliveInterval: keepaliveInterval,
		keepaliveTimeout:  keepaliveTimeout,
		pending:           newPendingMap(),
		signalingChs:      make(map[string]chan *gen.AcceleratorMessage),
		ctx:               ctx,
		cancel:            cancel,
		log:               log,
	}
}

func (s *AcceleratorSession) touch() {
	s.lastSeenMu.Lock()
	s.lastSeen = time.Now()
	s.lastSeenMu.Unlock()
}

func (s *AcceleratorSession) timeSinceLastSeen() time.Duration {
	s.lastSeenMu.RLock()
	defer s.lastSeenMu.RUnlock()
	return time.Since(s.lastSeen)
}

func (s *AcceleratorSession) isStale() bool {
	return s.timeSinceLastSeen() > s.keepaliveTimeout
}

func (s *AcceleratorSession) runKeepaliveSender() {
	ticker := time.NewTicker(s.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			msg := &gen.AcceleratorMessage{
				CommandId: uuid.NewString(),
				Payload: &gen.AcceleratorMessage_Keepalive{
					Keepalive: &gen.Keepalive{
						SenderUnixMs: time.Now().UnixMilli(),
					},
				},
			}
			if err := s.Send(msg); err != nil {
				s.log.Warn().Err(err).Msg("failed to send keepalive to accelerator")
				s.cancel()
				return
			}
			s.log.Debug().Msg("keepalive sent to accelerator")
		}
	}
}

func (s *AcceleratorSession) runStaleWatchdog() {
	tick := max(s.keepaliveInterval/2, time.Second)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.isStale() {
				s.log.Warn().
					Dur("since_last_seen", s.timeSinceLastSeen()).
					Dur("timeout", s.keepaliveTimeout).
					Msg("accelerator keepalive timeout")
				s.cancel()
				return
			}
		}
	}
}

// Send writes an envelope to the accelerator. Thread-safe.
func (s *AcceleratorSession) Send(msg *gen.AcceleratorMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.stream.Send(&gen.ConnectResponse{Message: msg})
}

// Await blocks until a response with the given command_id arrives, or ctx is
// done, or the session terminates.
func (s *AcceleratorSession) Await(ctx context.Context, commandID string) (*gen.AcceleratorMessage, error) {
	ch := s.pending.register(commandID)
	defer s.pending.cancel(commandID)
	select {
	case msg := <-ch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, errors.New("accelerator session terminated")
	}
}

// SubscribeSignaling registers a channel that receives inbound SignalingMessage
// envelopes from the accelerator.  Returns the channel and an unsubscribe func.
func (s *AcceleratorSession) SubscribeSignaling(subscriberID string) (<-chan *gen.AcceleratorMessage, func()) {
	ch := make(chan *gen.AcceleratorMessage, 32)
	s.signalingMu.Lock()
	s.signalingChs[subscriberID] = ch
	s.signalingMu.Unlock()
	return ch, func() { s.UnsubscribeSignaling(subscriberID) }
}

// UnsubscribeSignaling removes the subscriber and closes its channel.
func (s *AcceleratorSession) UnsubscribeSignaling(subscriberID string) {
	s.signalingMu.Lock()
	defer s.signalingMu.Unlock()
	if ch, ok := s.signalingChs[subscriberID]; ok {
		close(ch)
		delete(s.signalingChs, subscriberID)
	}
}

// deliverSignaling fans out a signaling envelope to all active subscribers.
func (s *AcceleratorSession) deliverSignaling(msg *gen.AcceleratorMessage) {
	s.signalingMu.RLock()
	defer s.signalingMu.RUnlock()
	for _, ch := range s.signalingChs {
		select {
		case ch <- msg:
		default:
			s.log.Warn().Msg("signaling subscriber channel full; dropping message")
		}
	}
}

// closeAllSignaling closes every subscriber channel on session teardown.
func (s *AcceleratorSession) closeAllSignaling() {
	s.signalingMu.Lock()
	defer s.signalingMu.Unlock()
	for id, ch := range s.signalingChs {
		close(ch)
		delete(s.signalingChs, id)
	}
}

// ----- pendingMap -----

type pendingMap struct {
	mu sync.Mutex
	m  map[string]chan *gen.AcceleratorMessage
}

func newPendingMap() *pendingMap {
	return &pendingMap{m: make(map[string]chan *gen.AcceleratorMessage)}
}

func (p *pendingMap) register(commandID string) <-chan *gen.AcceleratorMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan *gen.AcceleratorMessage, 1)
	p.m[commandID] = ch
	return ch
}

// deliver sends msg to the waiting goroutine. Returns false if no waiter exists.
func (p *pendingMap) deliver(commandID string, msg *gen.AcceleratorMessage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.m[commandID]
	if !ok {
		return false
	}
	select {
	case ch <- msg:
	default:
	}
	return true
}

func (p *pendingMap) cancel(commandID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, commandID)
}

// cancelAll closes all pending channels; called on session teardown.
func (p *pendingMap) cancelAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, ch := range p.m {
		close(ch)
		delete(p.m, id)
	}
}
