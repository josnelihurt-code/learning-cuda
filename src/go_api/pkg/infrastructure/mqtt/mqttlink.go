package mqtt

import (
	"sync"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"github.com/rs/zerolog/log"
)

// mqttLink owns the optional MQTT client. It is always non-nil on a DeviceMonitor
// and hides disconnected / misconfigured state from callers.
type mqttLink struct {
	mu     sync.Mutex
	client *client
}

func newMQTTLink(cfg config.MQTTConfig) *mqttLink {
	l := &mqttLink{}
	if cfg.Broker == "" {
		logger.Global().Info().Msg("MQTT broker not configured; device monitor disabled")
		return l
	}
	c, err := newClient(cfg)
	if err != nil {
		logger.Global().Warn().Err(err).Str("broker", cfg.Broker).Int("port", cfg.Port).
			Msg("MQTT broker unreachable; device monitor disabled")
		return l
	}
	l.client = c
	return l
}

func (l *mqttLink) connected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.client != nil
}

func (l *mqttLink) disconnect() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		l.client.Disconnect()
		l.client = nil
	}
}

func (l *mqttLink) subscribeSensorWithRaw(cb func(sensorData) error) error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.SubscribeToSensorWithRaw(cb)
}

func (l *mqttLink) subscribeInfo1(cb func(info1Data) error) error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.SubscribeToInfo1(cb)
}

func (l *mqttLink) subscribeInfo2(cb func(info2Data) error) error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.SubscribeToInfo2(cb)
}

func (l *mqttLink) subscribeLWT(cb func(string) error) error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.SubscribeToLWT(cb)
}

func (l *mqttLink) restartDevice() error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.RestartDevice()
}

func (l *mqttLink) publishPower(on bool) error {
	l.mu.Lock()
	c := l.client
	l.mu.Unlock()
	if c == nil {
		logger.Global().Warn().Msg("MQTT client not available; power command skipped")
		return nil
	}
	if on {
		log.Info().Msg("Powering on Jetson Nano on MQTT")
	} else {
		log.Info().Msg("Powering off Jetson Nano on MQTT")
	}
	return c.PublishPowerCommand(on)
}
