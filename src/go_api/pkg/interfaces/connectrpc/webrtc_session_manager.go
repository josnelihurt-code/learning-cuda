package connectrpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"google.golang.org/protobuf/proto"
)

const defaultWebRTCPollTimeout = 25 * time.Second

var (
	errSignalingSessionNotFound = errors.New("signaling session not found")
	errSignalingSessionClosed   = errors.New("signaling session closed")
)

type queuedSignalingEvent struct {
	cursor  int64
	message *pb.SignalingMessage
}

type webRTCSignalingSession struct {
	id           string
	stream       pb.WebRTCSignalingService_SignalingStreamClient
	cancel       context.CancelFunc
	closeOnce    sync.Once
	onTerminated func(*webRTCSignalingSession)

	sendMu sync.Mutex

	mu         sync.Mutex
	events     []queuedSignalingEvent
	nextCursor int64
	notifyCh   chan struct{}
	closed     bool
	lastErr    error
}

func newWebRTCSignalingSession(
	sessionID string,
	client WebRTCSignalingClient,
	onTerminated func(*webRTCSignalingSession),
) (*webRTCSignalingSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.SignalingStream(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	session := &webRTCSignalingSession{
		id:           sessionID,
		stream:       stream,
		cancel:       cancel,
		notifyCh:     make(chan struct{}),
		onTerminated: onTerminated,
	}

	go session.receiveLoop()

	return session, nil
}

// signalingMessageSessionID returns the session ID embedded in msg, or "" for
// message types that carry no session ID (e.g. KeepAlive).
func signalingMessageSessionID(msg *pb.SignalingMessage) string {
	switch m := msg.GetMessage().(type) {
	case *pb.SignalingMessage_StartSessionResponse:
		return m.StartSessionResponse.GetSessionId()
	case *pb.SignalingMessage_IceCandidate:
		return m.IceCandidate.GetSessionId()
	case *pb.SignalingMessage_IceCandidateResponse:
		return m.IceCandidateResponse.GetSessionId()
	case *pb.SignalingMessage_CloseSessionResponse:
		return m.CloseSessionResponse.GetSessionId()
	default:
		return ""
	}
}

func (s *webRTCSignalingSession) receiveLoop() {
	for {
		msg, err := s.stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Global().Warn().
					Err(err).
					Str("session_id", s.id).
					Msg("WebRTC signaling session closed with error")
			}
			s.markClosed(err)
			return
		}

		// The C++ fanout delivers all sessions' messages on every subscriber channel.
		// Drop messages whose session_id doesn't match this session to prevent one
		// session's close/response from bleeding into another session's event queue.
		if msgID := signalingMessageSessionID(msg); msgID != "" && msgID != s.id {
			continue
		}

		s.appendEvent(msg)
	}
}

func (s *webRTCSignalingSession) appendEvent(msg *pb.SignalingMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	cloned := proto.Clone(msg).(*pb.SignalingMessage)
	s.nextCursor++
	s.events = append(s.events, queuedSignalingEvent{
		cursor:  s.nextCursor,
		message: cloned,
	})

	close(s.notifyCh)
	s.notifyCh = make(chan struct{})
}

func (s *webRTCSignalingSession) removeEvent(cursor int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.events[:0]
	for _, event := range s.events {
		if event.cursor != cursor {
			filtered = append(filtered, event)
		}
	}
	s.events = filtered
}

// markClosed runs onTerminated under s.mu; the callback must not re-enter
// the session, keeping the lock order session.mu -> manager.mu.
func (s *webRTCSignalingSession) markClosed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	s.lastErr = err
	close(s.notifyCh)

	if s.onTerminated != nil {
		s.onTerminated(s)
	}
}

func (s *webRTCSignalingSession) shutdown() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.sendMu.Lock()
		defer s.sendMu.Unlock()
		if err := s.stream.CloseSend(); err != nil {
			logger.Global().Warn().
				Err(err).
				Str("session_id", s.id).
				Msg("Failed to close WebRTC signaling stream")
		}
	})
}

func (s *webRTCSignalingSession) currentCursor() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextCursor
}

func (s *webRTCSignalingSession) send(msg *pb.SignalingMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.stream.Send(msg)
}

func (s *webRTCSignalingSession) waitForEvents(
	ctx context.Context,
	cursor int64,
) ([]queuedSignalingEvent, int64, error) {
	for {
		s.mu.Lock()
		var events []queuedSignalingEvent
		for _, event := range s.events {
			if event.cursor > cursor {
				events = append(events, event)
			}
		}

		if len(events) > 0 {
			nextCursor := events[len(events)-1].cursor
			s.mu.Unlock()
			return events, nextCursor, nil
		}

		if s.closed {
			err := s.lastErr
			s.mu.Unlock()
			if err == nil || errors.Is(err, io.EOF) {
				return nil, cursor, errSignalingSessionClosed
			}
			return nil, cursor, err
		}

		notifyCh := s.notifyCh
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, cursor, ctx.Err()
		case <-notifyCh:
		}
	}
}

func (s *webRTCSignalingSession) waitForMatch(
	ctx context.Context,
	cursor int64,
	match func(*pb.SignalingMessage) bool,
) (*pb.SignalingMessage, int64, error) {
	currentCursor := cursor

	for {
		events, nextCursor, err := s.waitForEvents(ctx, currentCursor)
		if err != nil {
			return nil, currentCursor, err
		}

		for _, event := range events {
			currentCursor = event.cursor
			if match(event.message) {
				s.removeEvent(event.cursor)
				return event.message, currentCursor, nil
			}
		}

		currentCursor = nextCursor
	}
}

type WebRTCSignalingSessionManager struct {
	client      WebRTCSignalingClient
	pollTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*webRTCSignalingSession
}

func NewWebRTCSignalingSessionManager(client WebRTCSignalingClient) *WebRTCSignalingSessionManager {
	return &WebRTCSignalingSessionManager{
		client:      client,
		pollTimeout: defaultWebRTCPollTimeout,
		sessions:    make(map[string]*webRTCSignalingSession),
	}
}

func (m *WebRTCSignalingSessionManager) getSession(sessionID string) (*webRTCSignalingSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, errSignalingSessionNotFound
	}

	return session, nil
}

func (m *WebRTCSignalingSessionManager) createSession(sessionID string) (*webRTCSignalingSession, error) {
	m.mu.Lock()
	existing, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if ok {
		existing.shutdown()
	}

	session, err := newWebRTCSignalingSession(sessionID, m.client, func(s *webRTCSignalingSession) {
		m.evictIfCurrent(sessionID, s)
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	return session, nil
}

// evictIfCurrent matches by pointer identity so a late termination cannot
// evict a replacement installed under the same session ID.
func (m *WebRTCSignalingSessionManager) evictIfCurrent(sessionID string, session *webRTCSignalingSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current, ok := m.sessions[sessionID]; ok && current == session {
		delete(m.sessions, sessionID)
	}
}

func (m *WebRTCSignalingSessionManager) removeSession(sessionID string) {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if ok {
		session.shutdown()
	}
}

func (m *WebRTCSignalingSessionManager) StartSession(
	ctx context.Context,
	req *pb.StartSessionRequest,
) (*pb.StartSessionResponse, error) {
	if req.GetSessionId() == "" {
		return nil, errors.New("session_id is required")
	}
	if req.GetSdpOffer() == "" {
		return nil, errors.New("sdp_offer is required")
	}

	session, err := m.createSession(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	if err := session.send(&pb.SignalingMessage{
		Message: &pb.SignalingMessage_StartSession{
			StartSession: req,
		},
	}); err != nil {
		m.removeSession(req.GetSessionId())
		return nil, err
	}

	responseMessage, _, err := session.waitForMatch(ctx, 0, func(msg *pb.SignalingMessage) bool {
		response := msg.GetStartSessionResponse()
		return response != nil && response.GetSessionId() == req.GetSessionId()
	})
	if err != nil {
		m.removeSession(req.GetSessionId())
		return nil, err
	}

	return responseMessage.GetStartSessionResponse(), nil
}

func (m *WebRTCSignalingSessionManager) SendIceCandidate(
	ctx context.Context,
	req *pb.SendIceCandidateRequest,
) (*pb.SendIceCandidateResponse, error) {
	if req.GetSessionId() == "" {
		return nil, errors.New("session_id is required")
	}
	if req.GetCandidate() == nil {
		return nil, errors.New("candidate is required")
	}

	session, err := m.getSession(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	cursor := session.currentCursor()
	if err := session.send(&pb.SignalingMessage{
		Message: &pb.SignalingMessage_IceCandidate{
			IceCandidate: req,
		},
	}); err != nil {
		return nil, err
	}

	responseMessage, _, err := session.waitForMatch(ctx, cursor, func(msg *pb.SignalingMessage) bool {
		response := msg.GetIceCandidateResponse()
		return response != nil && response.GetSessionId() == req.GetSessionId()
	})
	if err != nil {
		return nil, err
	}

	return responseMessage.GetIceCandidateResponse(), nil
}

func (m *WebRTCSignalingSessionManager) PollEvents(
	ctx context.Context,
	req *pb.PollEventsRequest,
) (*pb.PollEventsResponse, error) {
	if req.GetSessionId() == "" {
		return nil, errors.New("session_id is required")
	}
	if req.GetCursor() < 0 {
		return nil, errors.New("cursor must be non-negative")
	}

	session, err := m.getSession(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	timeout := m.pollTimeout
	if req.GetTimeoutMs() > 0 {
		requested := time.Duration(req.GetTimeoutMs()) * time.Millisecond
		if requested < timeout {
			timeout = requested
		}
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	events, nextCursor, err := session.waitForEvents(pollCtx, req.GetCursor())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return &pb.PollEventsResponse{
				Events:        []*pb.SignalingMessage{},
				NextCursor:    req.GetCursor(),
				PollTimeoutMs: timeout.Milliseconds(),
				TraceContext:  req.GetTraceContext(),
			}, nil
		}
		return nil, err
	}

	messages := make([]*pb.SignalingMessage, 0, len(events))
	for _, event := range events {
		messages = append(messages, proto.Clone(event.message).(*pb.SignalingMessage))
	}

	return &pb.PollEventsResponse{
		Events:        messages,
		NextCursor:    nextCursor,
		PollTimeoutMs: timeout.Milliseconds(),
		TraceContext:  req.GetTraceContext(),
	}, nil
}

func (m *WebRTCSignalingSessionManager) CloseSession(
	ctx context.Context,
	req *pb.CloseSessionRequest,
) (*pb.CloseSessionResponse, error) {
	if req.GetSessionId() == "" {
		return nil, errors.New("session_id is required")
	}

	session, err := m.getSession(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	cursor := session.currentCursor()
	if err := session.send(&pb.SignalingMessage{
		Message: &pb.SignalingMessage_CloseSession{
			CloseSession: req,
		},
	}); err != nil {
		m.removeSession(req.GetSessionId())
		return nil, err
	}

	responseMessage, _, err := session.waitForMatch(ctx, cursor, func(msg *pb.SignalingMessage) bool {
		response := msg.GetCloseSessionResponse()
		return response != nil && response.GetSessionId() == req.GetSessionId()
	})
	m.removeSession(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	return responseMessage.GetCloseSessionResponse(), nil
}
