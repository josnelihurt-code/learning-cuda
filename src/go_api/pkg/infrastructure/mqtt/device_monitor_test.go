package mqtt

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

// A parseable Time keeps handleSensorData deterministic instead of falling back to time.Now.
func sensorDataFor(power, voltage float64) SensorData {
	var data SensorData
	data.Time = "2026-09-19T10:00:00"
	data.ENERGY.Power = power
	data.ENERGY.Voltage = voltage
	return data
}

func TestSuccess_SubscriberReceivesStableSnapshot(t *testing.T) {
	// Arrange
	sut := NewDeviceMonitor(t.Context(), config.MQTTConfig{})

	received := make([]*domain.DeviceStatus, 0, 2)
	unsubscribe := sut.Subscribe(func(status *domain.DeviceStatus) {
		received = append(received, status)
	})
	defer unsubscribe()

	// Act
	sut.handleSensorData(sensorDataFor(42.5, 220))

	// Assert
	if len(received) != 2 {
		t.Fatalf("subscriber received %d snapshots, want 2 (initial + update)", len(received))
	}
	initial, updated := received[0], received[1]
	if initial == updated {
		t.Fatal("initial and updated snapshots are the same object; subscribers must receive distinct copies")
	}
	if initial.Power != 0 || initial.Voltage != 0 || initial.IsOnline {
		t.Fatalf("initial snapshot mutated after update: got Power=%v Voltage=%v IsOnline=%v, want zero values",
			initial.Power, initial.Voltage, initial.IsOnline)
	}
	if updated.LastSeen.IsZero() {
		t.Fatal("updated snapshot missing LastSeen from sensor update")
	}
	if updated.Power != 42.5 || updated.Voltage != 220 || !updated.IsOnline {
		t.Fatalf("updated snapshot = Power %v, Voltage %v, IsOnline %v; want 42.5, 220, true",
			updated.Power, updated.Voltage, updated.IsOnline)
	}
}

func TestSuccess_UnsubscribeRemovesOnlyOwnSubscriber(t *testing.T) {
	// Arrange
	sut := NewDeviceMonitor(t.Context(), config.MQTTConfig{})

	var callsA, callsB atomic.Int32
	unsubscribeA := sut.Subscribe(func(*domain.DeviceStatus) { callsA.Add(1) })
	sut.Subscribe(func(*domain.DeviceStatus) { callsB.Add(1) })

	// Act
	unsubscribeA()
	sut.handleSensorData(sensorDataFor(1, 2))

	// Assert
	if got := callsA.Load(); got != 1 {
		t.Fatalf("unsubscribed callback A invoked %d times, want 1 (initial snapshot only)", got)
	}
	if got := callsB.Load(); got != 2 {
		t.Fatalf("remaining callback B invoked %d times, want 2 (initial snapshot + update)", got)
	}
}

func TestSuccess_ConcurrentSubscribersAndUpdates(t *testing.T) {
	// Arrange
	sut := NewDeviceMonitor(t.Context(), config.MQTTConfig{})

	const (
		churners   = 4
		updaters   = 2
		iterations = 50
	)

	var wg sync.WaitGroup
	var deliveries atomic.Int32

	// Act
	for range churners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				unsubscribe := sut.Subscribe(func(status *domain.DeviceStatus) {
					deliveries.Add(1)
					_ = status.String()
				})
				unsubscribe()
			}
		}()
	}
	for range updaters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range iterations {
				sut.handleSensorData(sensorDataFor(float64(i), float64(i)))
				sut.handleLWTStatus("Online")
			}
		}()
	}
	wg.Wait()

	// Assert
	if deliveries.Load() == 0 {
		t.Fatal("expected at least one subscriber delivery")
	}
	sut.subscribersMu.RLock()
	remaining := len(sut.subscribers)
	sut.subscribersMu.RUnlock()
	if remaining != 0 {
		t.Fatalf("%d subscribers remained after every unsubscribe, want 0", remaining)
	}
}
