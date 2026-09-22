package connectrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedSignalingStream scripts Recv; Send and CloseSend succeed
// silently, so the embedded interface stays nil.
type scriptedSignalingStream struct {
	pb.WebRTCSignalingService_SignalingStreamClient
	recv func() (*pb.SignalingMessage, error)
}

func newScriptedSignalingStream(recv func() (*pb.SignalingMessage, error)) *scriptedSignalingStream {
	return &scriptedSignalingStream{recv: recv}
}

func (s *scriptedSignalingStream) Recv() (*pb.SignalingMessage, error) { return s.recv() }
func (s *scriptedSignalingStream) Send(_ *pb.SignalingMessage) error   { return nil }
func (s *scriptedSignalingStream) CloseSend() error                    { return nil }

type scriptedSignalingClient struct {
	streams []*scriptedSignalingStream
	next    int
}

func (c *scriptedSignalingClient) SignalingStream(context.Context) (pb.WebRTCSignalingService_SignalingStreamClient, error) {
	if c.next >= len(c.streams) {
		return nil, errors.New("scriptedSignalingClient: no more scripted streams")
	}
	stream := c.streams[c.next]
	c.next++
	return stream, nil
}

// blockingRecv makes stream death a scripted event, so the ordering of
// "replacement installed" vs "old stream dies" is deterministic.
func blockingRecv(dieErr error) (recv func() (*pb.SignalingMessage, error), entered, released chan struct{}) {
	entered = make(chan struct{})
	released = make(chan struct{})
	recv = func() (*pb.SignalingMessage, error) {
		close(entered)
		<-released
		return nil, dieErr
	}
	return recv, entered, released
}

func managerSessionEntry(m *webRTCSignalingSessionManager, sessionID string) (*webRTCSignalingSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	return session, ok
}

func TestSuccess_SessionEvictedWhenStreamDies(t *testing.T) {
	// Arrange:
	streamErr := errors.New("connection reset by peer")
	recv, entered, released := blockingRecv(streamErr)
	sut := newWebRTCSignalingSessionManager(&scriptedSignalingClient{
		streams: []*scriptedSignalingStream{newScriptedSignalingStream(recv)},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	// Act:
	session, err := sut.createSession("session-1")
	require.NoError(t, err)

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("timed out waiting for the receive loop to enter Recv")
	}

	close(released)

	// Assert:
	_, _, waitErr := session.waitForEvents(ctx, session.currentCursor())
	require.ErrorIs(t, waitErr, streamErr)

	current, ok := managerSessionEntry(sut, "session-1")
	assert.False(t, ok, "session must be evicted when the upstream stream dies, got %v", current)
}

func TestSuccess_LateTerminationDoesNotEvictReplacement(t *testing.T) {
	// Arrange:
	const sessionID = "session-1"

	streamErrA := errors.New("connection reset by peer")
	recvA, _, releasedA := blockingRecv(streamErrA)
	recvB, _, releasedB := blockingRecv(errors.New("connection reset by peer"))
	sut := newWebRTCSignalingSessionManager(&scriptedSignalingClient{
		streams: []*scriptedSignalingStream{
			newScriptedSignalingStream(recvA),
			newScriptedSignalingStream(recvB),
		},
	})
	t.Cleanup(func() { close(releasedB) }) // let B's blocked Recv exit after the test

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	sessionA, err := sut.createSession(sessionID)
	require.NoError(t, err)

	// Act:
	sessionB, err := sut.createSession(sessionID)
	require.NoError(t, err)

	current, ok := managerSessionEntry(sut, sessionID)
	require.True(t, ok, "precondition: the replacement session must be registered")
	require.Same(t, sessionB, current)

	close(releasedA)

	// Assert:
	_, _, waitErr := sessionA.waitForEvents(ctx, sessionA.currentCursor())
	require.ErrorIs(t, waitErr, streamErrA)

	current, ok = managerSessionEntry(sut, sessionID)
	require.True(t, ok, "the replacement session must survive the old session's late termination")
	assert.Same(t, sessionB, current)
}
