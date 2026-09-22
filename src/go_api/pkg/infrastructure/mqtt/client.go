package mqtt

import (
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/rs/zerolog/log"
)

type client struct {
	client mqtt.Client
	config config.MQTTConfig
}

type sensorData struct {
	Time   string `json:"Time"`
	ENERGY struct {
		Power   float64 `json:"Power"`
		Voltage float64 `json:"Voltage"`
	} `json:"ENERGY"`
}

type info1Data struct {
	Info1 struct {
		Module  string `json:"Module"`
		Version string `json:"Version"`
	} `json:"Info1"`
}

type info2Data struct {
	Info2 struct {
		Hostname  string `json:"Hostname"`
		IPAddress string `json:"IPAddress"`
	} `json:"Info2"`
}

func newClient(cfg config.MQTTConfig) (*client, error) {
	brokerURL := fmt.Sprintf("tcp://%s:%d", cfg.Broker, cfg.Port)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(false)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetWriteTimeout(10 * time.Second)

	pahoClient := mqtt.NewClient(opts)

	if token := pahoClient.Connect(); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	if !pahoClient.IsConnected() {
		return nil, fmt.Errorf("MQTT client not connected after Connect()")
	}

	log.Info().Str("broker", cfg.Broker).Int("port", cfg.Port).Str("client_id", cfg.ClientID).Msg("MQTT client connected")
	time.Sleep(500 * time.Millisecond)

	return &client{
		client: pahoClient,
		config: cfg,
	}, nil
}

func (c *client) PublishPowerCommand(on bool) error {
	if !on {
		return nil //DISABLED
	}
	topic := fmt.Sprintf("cmnd/%s/POWER", c.config.Topic)
	payload := "OFF"
	if on {
		payload = "ON"
	}

	token := c.client.Publish(topic, 0, false, payload)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to publish power command: %w", token.Error())
	}

	log.Info().Str("topic", topic).Str("payload", payload).Msg("Power command published")

	return nil
}

func (c *client) SubscribeToSensorWithRaw(callback func(data sensorData) error) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("MQTT client not connected")
	}

	topic := fmt.Sprintf("tele/%s/SENSOR", c.config.Topic)

	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		var sensorData sensorData
		if err := json.Unmarshal(msg.Payload(), &sensorData); err != nil {
			return
		}

		if err := callback(sensorData); err != nil {
			return
		}
	}

	token := c.client.Subscribe(topic, 0, messageHandler)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("timeout waiting for subscribe to sensor topic")
	}

	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to sensor topic: %w", token.Error())
	}

	log.Info().Str("topic", topic).Msg("Subscribed to sensor topic with raw")

	return nil
}

func (c *client) SubscribeToInfo1(callback func(data info1Data) error) error {
	topic := fmt.Sprintf("tele/%s/INFO1", c.config.Topic)

	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		var info1Data info1Data
		if err := json.Unmarshal(msg.Payload(), &info1Data); err != nil {
			return
		}

		if err := callback(info1Data); err != nil {
			return
		}
	}

	token := c.client.Subscribe(topic, 0, messageHandler)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to INFO1 topic: %w", token.Error())
	}

	log.Info().Str("topic", topic).Msg("Subscribed to INFO1 topic")

	return nil
}

func (c *client) SubscribeToInfo2(callback func(data info2Data) error) error {
	topic := fmt.Sprintf("tele/%s/INFO2", c.config.Topic)

	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		var info2Data info2Data
		if err := json.Unmarshal(msg.Payload(), &info2Data); err != nil {
			return
		}

		if err := callback(info2Data); err != nil {
			return
		}
	}

	token := c.client.Subscribe(topic, 0, messageHandler)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to INFO2 topic: %w", token.Error())
	}

	log.Info().Str("topic", topic).Msg("Subscribed to INFO2 topic")

	return nil
}

func (c *client) SubscribeToLWT(callback func(status string) error) error {
	topic := fmt.Sprintf("tele/%s/LWT", c.config.Topic)

	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		status := string(msg.Payload())
		if err := callback(status); err != nil {
			return
		}
	}

	token := c.client.Subscribe(topic, 0, messageHandler)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to LWT topic: %w", token.Error())
	}

	log.Info().Str("topic", topic).Msg("Subscribed to LWT topic")

	return nil
}

func (c *client) RestartDevice() error {
	return nil //DISABLED
}

func (c *client) Disconnect() {
	if c == nil || c.client == nil {
		return
	}
	log.Info().Msg("Disconnecting MQTT client")
	c.client.Disconnect(250)
	log.Info().Msg("MQTT client disconnected")
}
