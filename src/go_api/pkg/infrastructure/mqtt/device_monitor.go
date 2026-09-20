package mqtt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

type DeviceMonitor struct {
	link          *mqttLink
	status        *domain.DeviceStatus
	mu            sync.RWMutex
	subscribers   []func(*domain.DeviceStatus)
	subscribersMu sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	started       bool
	startedMu     sync.RWMutex
}

// NewDeviceMonitor always returns a monitor backed by an mqttLink. The link may
// have no live MQTT session; connection details stay inside mqttLink.
func NewDeviceMonitor(ctx context.Context, cfg config.MQTTConfig) *DeviceMonitor {
	baseCtx, cancel := context.WithCancel(ctx)
	return &DeviceMonitor{
		link:        newMQTTLink(cfg),
		status:      domain.NewDeviceStatus(),
		subscribers: make([]func(*domain.DeviceStatus), 0),
		ctx:         baseCtx,
		cancel:      cancel,
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

// dropPump adapts an MQTT callback subscription to a buffered channel,
// dropping messages when the consumer falls behind.
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
	statusCopy := dm.status
	dm.mu.Unlock()

	dm.notify(statusCopy)
}

func (dm *DeviceMonitor) handleInfo1Data(data Info1Data) {
	dm.mu.Lock()
	dm.status.UpdateInfo1(data.Info1.Version, data.Info1.Module)
	statusCopy := dm.status
	dm.mu.Unlock()

	dm.notify(statusCopy)
}

func (dm *DeviceMonitor) handleInfo2Data(data Info2Data) {
	dm.mu.Lock()
	dm.status.UpdateInfo2(data.Info2.Hostname, data.Info2.IPAddress)
	statusCopy := dm.status
	dm.mu.Unlock()

	dm.notify(statusCopy)
}

func (dm *DeviceMonitor) handleLWTStatus(status string) {
	dm.mu.Lock()
	dm.status.UpdateLWTStatus(status)
	statusCopy := dm.status
	dm.mu.Unlock()

	dm.notify(statusCopy)
}

func (dm *DeviceMonitor) notify(status *domain.DeviceStatus) {
	dm.subscribersMu.RLock()
	subscribers := make([]func(*domain.DeviceStatus), len(dm.subscribers))
	copy(subscribers, dm.subscribers)
	dm.subscribersMu.RUnlock()

	for _, callback := range subscribers {
		callback(status)
	}
}

func (dm *DeviceMonitor) PowerOn() error {
	return dm.link.publishPower(true)
}

func (dm *DeviceMonitor) Subscribe(callback func(*domain.DeviceStatus)) func() {
	statusCopy := dm.status
	callback(statusCopy)
	dm.subscribersMu.Lock()
	dm.subscribers = append(dm.subscribers, callback)
	index := len(dm.subscribers) - 1
	dm.subscribersMu.Unlock()

	return func() {
		dm.subscribersMu.Lock()
		defer dm.subscribersMu.Unlock()
		if index < len(dm.subscribers) {
			dm.subscribers = append(dm.subscribers[:index], dm.subscribers[index+1:]...)
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

	dm.cancel()
	dm.link.disconnect()
	return nil
}
