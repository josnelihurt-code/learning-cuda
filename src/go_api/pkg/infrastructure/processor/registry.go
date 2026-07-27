package processor

import (
	"errors"
	"sync"

	"github.com/rs/zerolog"
)

// Registry holds the set of registered accelerator sessions.
// v1 enforces exactly one session at a time; the map shape supports v2 multi-device.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*AcceleratorSession
	log      zerolog.Logger
}

func NewRegistry(log zerolog.Logger) *Registry {
	return &Registry{
		sessions: make(map[string]*AcceleratorSession),
		log:      log,
	}
}

// Add registers a new session. Returns an error if a *different* accelerator is
// already registered (v1 single-accelerator policy). A reconnect from the same
// device evicts its own stale session instead of being rejected: otherwise a
// reconnect that beats the old stream's teardown gets AlreadyExists and the
// device backs off, staying offline for no reason.
func (r *Registry) Add(s *AcceleratorSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for deviceID := range r.sessions {
		if deviceID != s.DeviceID {
			return errors.New("v1 supports only one accelerator at a time")
		}
	}
	if prev, ok := r.sessions[s.DeviceID]; ok {
		r.log.Warn().
			Str("device_id", s.DeviceID).
			Str("previous_session_id", prev.AssignedSession).
			Msg("evicting stale accelerator session on re-register")
		prev.cancel()
	}
	r.sessions[s.DeviceID] = s
	r.log.Info().Str("device_id", s.DeviceID).Msg("accelerator session registered")
	return nil
}

// Remove removes the session for the given device_id, but only if the currently
// registered session is the one passed in. Removing by device_id alone lets a
// slow teardown of an old stream delete the entry a newer stream just created —
// the accelerator then believes it is registered while the API reports none.
func (r *Registry) Remove(deviceID string, s *AcceleratorSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[deviceID]
	if !ok {
		return
	}
	if s != nil && current != s {
		r.log.Warn().
			Str("device_id", deviceID).
			Str("assigned_session_id", s.AssignedSession).
			Msg("skipping remove: a newer accelerator session is registered")
		return
	}
	delete(r.sessions, deviceID)
	r.log.Info().Str("device_id", deviceID).Msg("accelerator session removed")
}

// Get returns the session for the given device_id, or nil + false.
func (r *Registry) Get(deviceID string) (*AcceleratorSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[deviceID]
	return s, ok
}

// First returns the singleton session in v1, or nil + false if none registered.
// v2 will deprecate this in favour of explicit device selection.
func (r *Registry) First() (*AcceleratorSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sessions {
		return s, true
	}
	return nil, false
}
