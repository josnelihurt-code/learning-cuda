package mqtt

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

// The id, not a slice index, identifies the entry: indices shift on removal
// and func values are not comparable.
type deviceSubscriber struct {
	id       uint64
	callback func(*domain.DeviceStatus)
}

type DeviceMonitor struct {
	link             *mqttLink
	status           *domain.DeviceStatus
	mu               sync.RWMutex
	subscribers      []deviceSubscriber
	nextSubscriberID atomic.Uint64
	subscribersMu    sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	started          bool
	startedMu        sync.RWMutex
}

// NewDeviceMonitor never fails; the returned monitor's mqttLink may not have
// a live MQTT session yet.
func NewDeviceMonitor(ctx context.Context, cfg config.MQTTConfig) *DeviceMonitor {
	return &DeviceMonitor{
		link:        newMQTTLink(cfg),
		status:      domain.NewDeviceStatus(),
		subscribers: make([]deviceSubscriber, 0),
		ctx:         ctx,
	}
}

func (dm *DeviceMonitor) Start(ctx context.Context) error {
	dm.startedMu.Lock()
	if dm.started {
		dm.startedMu.Unlock()
		return fmt.Errorf("monitor already started")
	}
	dm.started = true
	dm.startedMu.Unlock()

	dm.ctx, dm.cancel = context.WithCancel(ctx)

	if !dm.link.connected() {
		logger.Global().Warn().Msg("MQTT device monitor: no connection; skipping subscriptions")
		return nil
	}

	sensorChan := make(chan SensorData, 10)
	info1Chan := make(chan Info1Data, 10)
	info2Chan := make(chan Info2Data, 10)
	lwtChan := make(chan string, 10)

	if err := dm.link.subscribeSensorWithRaw(dropPump(sensorChan)); err != nil {
		logger.Global().Warn().Err(err).Msg("MQTT: subscribe to SENSOR failed; device monitor disabled")
		dm.link.disconnect()
		return nil
	}

	if err := dm.link.subscribeInfo1(dropPump(info1Chan)); err != nil {
		logger.Global().Warn().Err(err).Msg("MQTT: subscribe to INFO1 failed; device monitor disabled")
		dm.link.disconnect()
		return nil
	}

	if err := dm.link.subscribeInfo2(dropPump(info2Chan)); err != nil {
		logger.Global().Warn().Err(err).Msg("MQTT: subscribe to INFO2 failed; device monitor disabled")
		dm.link.disconnect()
		return nil
	}

	if err := dm.link.subscribeLWT(dropPump(lwtChan)); err != nil {
		logger.Global().Warn().Err(err).Msg("MQTT: subscribe to LWT failed; device monitor disabled")
		dm.link.disconnect()
		return nil
	}

	if err := dm.link.restartDevice(); err != nil {
		logger.Global().Warn().Err(err).Msg("MQTT: restart command failed; device monitor disabled")
		dm.link.disconnect()
		return nil
	}

	time.Sleep(3 * time.Second)

	go dm.monitorLoop(sensorChan, info1Chan, info2Chan, lwtChan)

	return nil
}

func dropPump[T any](out chan<- T) func(T) error {
	return func(v T) error {
		select {
		case out <- v:
		default:
		}
		return nil
	}
}

func (dm *DeviceMonitor) monitorLoop(sensorChan <-chan SensorData, info1Chan <-chan Info1Data, info2Chan <-chan Info2Data, lwtChan <-chan string) {
	for {
		select {
		case <-dm.ctx.Done():
			return
		case sensorData := <-sensorChan:
			dm.handleSensorData(sensorData)
		case info1Data := <-info1Chan:
			dm.handleInfo1Data(info1Data)
		case info2Data := <-info2Chan:
			dm.handleInfo2Data(info2Data)
		case lwtStatus := <-lwtChan:
			dm.handleLWTStatus(lwtStatus)
		}
	}
}

func (dm *DeviceMonitor) handleSensorData(data SensorData) {
	timestamp, err := time.Parse("2006-01-02T15:04:05", data.Time)
	if err != nil {
		timestamp = time.Now()
	}

	dm.mu.Lock()
	dm.status.UpdatePower(data.ENERGY.Power, timestamp)
	dm.status.UpdateVoltage(data.ENERGY.Voltage)
	snapshot := dm.status.Clone()
	dm.mu.Unlock()

	dm.notify(snapshot)
}

func (dm *DeviceMonitor) handleInfo1Data(data Info1Data) {
	dm.mu.Lock()
	dm.status.UpdateInfo1(data.Info1.Version, data.Info1.Module)
	snapshot := dm.status.Clone()
	dm.mu.Unlock()

	dm.notify(snapshot)
}

func (dm *DeviceMonitor) handleInfo2Data(data Info2Data) {
	dm.mu.Lock()
	dm.status.UpdateInfo2(data.Info2.Hostname, data.Info2.IPAddress)
	snapshot := dm.status.Clone()
	dm.mu.Unlock()

	dm.notify(snapshot)
}

func (dm *DeviceMonitor) handleLWTStatus(status string) {
	dm.mu.Lock()
	dm.status.UpdateLWTStatus(status)
	snapshot := dm.status.Clone()
	dm.mu.Unlock()

	dm.notify(snapshot)
}

func (dm *DeviceMonitor) notify(status *domain.DeviceStatus) {
	dm.subscribersMu.RLock()
	subscribers := make([]deviceSubscriber, len(dm.subscribers))
	copy(subscribers, dm.subscribers)
	dm.subscribersMu.RUnlock()

	for _, sub := range subscribers {
		sub.callback(status)
	}
}

func (dm *DeviceMonitor) PowerOn() error {
	return dm.link.publishPower(true)
}

// Subscribe delivers the current status snapshot immediately, then future
// updates; the returned function removes only its own registration.
func (dm *DeviceMonitor) Subscribe(callback func(*domain.DeviceStatus)) func() {
	id := dm.nextSubscriberID.Add(1)

	dm.mu.RLock()
	snapshot := dm.status.Clone()
	dm.mu.RUnlock()
	callback(snapshot)

	dm.subscribersMu.Lock()
	dm.subscribers = append(dm.subscribers, deviceSubscriber{id: id, callback: callback})
	dm.subscribersMu.Unlock()

	return func() {
		dm.subscribersMu.Lock()
		defer dm.subscribersMu.Unlock()
		for i, sub := range dm.subscribers {
			if sub.id != id {
				continue
			}
			dm.subscribers = append(dm.subscribers[:i], dm.subscribers[i+1:]...)
			return
		}
	}
}

func (dm *DeviceMonitor) Stop() error {
	dm.startedMu.Lock()
	if !dm.started {
		dm.startedMu.Unlock()
		return nil
	}
	dm.started = false
	dm.startedMu.Unlock()

	if dm.cancel != nil {
		dm.cancel()
	}
	dm.link.disconnect()
	return nil
}
