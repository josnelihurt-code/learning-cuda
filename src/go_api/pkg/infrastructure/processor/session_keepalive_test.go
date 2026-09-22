package processor

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Manual integration (no automated harness yet):
//  1. Start Go API and accelerator_control_client; confirm debug keepalive logs both directions.
//  2. Kill Go API — client backs off and reconnects when API returns.
//  3. SIGTERM client — process exits within a few seconds (not stuck on Read).
func TestAcceleratorSession_touchAndStale(t *testing.T) {
	s := &acceleratorSession{
		lastSeen:          time.Now(),
		keepaliveTimeout:  45 * time.Second,
		keepaliveInterval: 15 * time.Second,
		log:               zerolog.Nop(),
	}

	if s.isStale() {
		t.Fatal("expected fresh session not to be stale")
	}

	s.lastSeenMu.Lock()
	s.lastSeen = time.Now().Add(-60 * time.Second)
	s.lastSeenMu.Unlock()

	if !s.isStale() {
		t.Fatal("expected session older than timeout to be stale")
	}

	s.touch()
	if s.isStale() {
		t.Fatal("expected touch to refresh lastSeen")
	}
}

func TestAcceleratorSession_timeSinceLastSeen(t *testing.T) {
	s := &acceleratorSession{
		lastSeen: time.Now().Add(-2 * time.Second),
		log:      zerolog.Nop(),
	}

	elapsed := s.timeSinceLastSeen()
	if elapsed < time.Second || elapsed > 5*time.Second {
		t.Fatalf("unexpected elapsed: %v", elapsed)
	}
}
