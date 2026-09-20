package config

import (
	"context"
	"log"
	"time"

	"github.com/spf13/viper"
)

type Manager struct {
	HTTPClientTimeout time.Duration       `mapstructure:"http_client_timeout"`
	Environment       string              `mapstructure:"environment"`
	GoFeatureFlag     GoFeatureFlagConfig `mapstructure:"go_feature_flag"`
	Server            ServerConfig        `mapstructure:"server"`
	Observability     ObservabilityConfig `mapstructure:"observability"`
	Logging           LoggerConfig        `mapstructure:"logging"`
	Processor         ProcessorConfig     `mapstructure:"processor"`
	Tools             ToolsConfig         `mapstructure:"tools"`
	StaticImages      StaticImagesConfig  `mapstructure:"static_images"`
	MQTT              MQTTConfig          `mapstructure:"mqtt"`
}

type GoFeatureFlagConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	FilePath  string `mapstructure:"file_path"`
	Namespace string `mapstructure:"namespace"`
}

type ToolsConfig struct {
	Observability []ToolDefinition
	Features      []ToolDefinition
	Testing       []ToolDefinition
}

type ToolDefinition struct {
	ID       string `mapstructure:"id"`
	Name     string `mapstructure:"name"`
	IconPath string `mapstructure:"icon_path"`
	Type     string `mapstructure:"type"`
	URL      string `mapstructure:"url"`
	Action   string `mapstructure:"action"`
}

type StaticImagesConfig struct {
	Directory string `mapstructure:"directory"`
}

func (m *Manager) IsObservabilityEnabled(ctx context.Context) bool {
	return m.Observability.Enabled
}

// Redacted returns a shallow copy with secret fields masked, safe to log.
func (m *Manager) Redacted() *Manager {
	c := *m
	if c.Observability.OtelAuthToken != "" {
		c.Observability.OtelAuthToken = "[redacted]"
	}
	return &c
}

func New(configFile string) *Manager {
	v := viper.New()

	if configFile == "" {
		configFile = "config/config.yaml"
	}

	v.SetConfigFile(configFile)
	v.SetConfigType("yaml")

	setDefaults(v)

	_ = v.BindEnv("processor.keepalive_interval", "PROCESSOR_KEEPALIVE_INTERVAL")
	_ = v.BindEnv("processor.keepalive_timeout", "PROCESSOR_KEEPALIVE_TIMEOUT")
	_ = v.BindEnv("observability.otel_auth_token", "OTEL_EXPORTER_OTLP_TOKEN")

	if err := v.ReadInConfig(); err != nil {
		log.Printf("Warning: Config file not found: %v, using defaults", err)
	}

	var cfg Manager
	if err := v.Unmarshal(&cfg); err != nil {
		log.Fatalf("Unable to unmarshal config: %v", err)
	}

	return &cfg
}

func setDefaults(v *viper.Viper) {
	defaults := map[string]interface{}{
		"environment":         "development",
		"http_client_timeout": "10s",

		"server.http_port":                 ":8080",
		"server.https_port":                ":8443",
		"server.webrtc_signaling_endpoint": "/cuda_learning.WebRTCSignalingService/StartSession",
		"server.tls.enabled":               true,
		"server.tls.cert_file":             ".secrets/localhost+2.pem",
		"server.tls.key_file":              ".secrets/localhost+2-key.pem",

		"observability.enabled":                      false,
		"observability.service_name":                 "cuda-image-processor",
		"observability.service_version":              "1.0.0",
		"observability.otel_exporter_protocol":       "grpc",
		"observability.otel_collector_grpc_endpoint": "localhost:4317",
		"observability.otel_collector_http_endpoint": "http://localhost:4318",
		"observability.trace_sampling_rate":          1.0,

		"go_feature_flag.enabled":   true,
		"go_feature_flag.file_path": "config/flags.goff.yaml",
		"go_feature_flag.namespace": "default",

		"logging.level":              "info",
		"logging.format":             "json",
		"logging.output":             "stdout",
		"logging.include_caller":     true,
		"logging.remote_enabled":     false,
		"logging.remote_environment": "development",

		"processor.library_base_path":   ".ignore/lib/cuda_learning",
		"processor.listen_address":      ":60062",
		"processor.keepalive_interval":  "15s",
		"processor.keepalive_timeout":   "45s",
		"processor.tls.cert_file":       ".secrets/accelerator-server.pem",
		"processor.tls.key_file":        ".secrets/accelerator-server-key.pem",
		"processor.tls.client_ca_file":  ".secrets/accelerator-ca.pem",

		"static_images.directory": "/data/static_images",

		"mqtt.broker":    "vultur.josnelihurt.me",
		"mqtt.port":      1883,
		"mqtt.client_id": "cuda-learning-remote-management",
		"mqtt.topic":     "pow/S31JetsonNanoOrin",
	}

	for key, value := range defaults {
		v.SetDefault(key, value)
	}
}
