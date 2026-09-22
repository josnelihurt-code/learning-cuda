package configquery

import "errors"

var ErrSignalingEndpointNotConfigured = errors.New("webrtc signaling endpoint not configured")

type streamSettingsSource interface {
	WebRTCSignalingEndpoint() string
}

type toolsCatalog interface {
	Environment() string
	ObservabilityTools() []Tool
	FeaturesTools() []Tool
	TestingTools() []Tool
}

type Tool struct {
	ID       string
	Name     string
	IconPath string
	Type     string
	URL      string
	Action   string
}

type ToolCategory struct {
	ID    string
	Name  string
	Tools []Tool
}
