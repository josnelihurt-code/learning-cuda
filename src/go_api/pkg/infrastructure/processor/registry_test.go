package processor

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func makeSession(deviceID, assignedID string) *AcceleratorSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &AcceleratorSession{
		DeviceID:        deviceID,
		AssignedSession: assignedID,
		ctx:             ctx,
		cancel:          cancel,
		log:             zerolog.Nop(),
	}
}

func TestSuccess_RegistryAddThenRemove(t *testing.T) {
	// Arrange
	sut := NewRegistry(zerolog.Nop())
	sess := makeSession("jetson-prod-01", "session-a")

	// Act
	err := sut.Add(sess)

	// Assert
	if err != nil {
		t.Fatalf("expected Add to succeed, got %v", err)
	}
	if _, ok := sut.First(); !ok {
		t.Fatal("expected session to be registered")
	}

	sut.Remove(sess.DeviceID, sess)
	if _, ok := sut.First(); ok {
		t.Fatal("expected session to be removed")
	}
}

func TestError_RegistryRejectsSecondDevice(t *testing.T) {
	// Arrange
	sut := NewRegistry(zerolog.Nop())
	first := makeSession("jetson-prod-01", "session-a")
	second := makeSession("jetson-prod-02", "session-b")

	// Act
	_ = sut.Add(first)
	err := sut.Add(second)

	// Assert
	if err == nil {
		t.Fatal("expected second distinct device to be rejected")
	}
}

func TestEdge_RegistryReRegisterSameDeviceEvictsStaleSession(t *testing.T) {
	// Arrange — a reconnect that beats the old stream's teardown.
	sut := NewRegistry(zerolog.Nop())
	old := makeSession("jetson-prod-01", "session-a")
	fresh := makeSession("jetson-prod-01", "session-b")
	if err := sut.Add(old); err != nil {
		t.Fatalf("arrange: %v", err)
	}

	// Act
	err := sut.Add(fresh)

	// Assert
	if err != nil {
		t.Fatalf("expected re-register of same device to succeed, got %v", err)
	}
	got, ok := sut.First()
	if !ok || got.AssignedSession != "session-b" {
		t.Fatalf("expected fresh session to be registered, got %+v (ok=%v)", got, ok)
	}
	select {
	case <-old.ctx.Done():
	default:
		t.Fatal("expected evicted session to be cancelled")
	}
}

func TestEdge_RegistryRemoveDoesNotDropNewerSession(t *testing.T) {
	// Arrange — old stream's deferred teardown runs after the reconnect landed.
	sut := NewRegistry(zerolog.Nop())
	old := makeSession("jetson-prod-01", "session-a")
	fresh := makeSession("jetson-prod-01", "session-b")
	if err := sut.Add(fresh); err != nil {
		t.Fatalf("arrange: %v", err)
	}

	// Act
	sut.Remove(old.DeviceID, old)

	// Assert
	got, ok := sut.First()
	if !ok || got.AssignedSession != "session-b" {
		t.Fatal("expected newer session to survive the stale teardown")
	}
}
