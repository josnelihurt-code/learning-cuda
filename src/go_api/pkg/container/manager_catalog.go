package container

import (
	"github.com/jrb/cuda-learning/src/go_api/pkg/application/platform/configquery"
	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
)

// managerCatalog adapts *config.Manager to configquery ports at the composition root.
type managerCatalog struct {
	manager *config.Manager
}

func (a *managerCatalog) WebRTCSignalingEndpoint() string {
	if a.manager == nil {
		return ""
	}
	return a.manager.Server.WebRTCSignalingEndpoint
}

func (a *managerCatalog) Environment() string {
	if a.manager == nil {
		return ""
	}
	return a.manager.Environment
}

func (a *managerCatalog) ObservabilityTools() []configquery.Tool {
	if a.manager == nil {
		return nil
	}
	return mapConfigTools(a.manager.Tools.Observability)
}

func (a *managerCatalog) FeaturesTools() []configquery.Tool {
	if a.manager == nil {
		return nil
	}
	return mapConfigTools(a.manager.Tools.Features)
}

func (a *managerCatalog) TestingTools() []configquery.Tool {
	if a.manager == nil {
		return nil
	}
	return mapConfigTools(a.manager.Tools.Testing)
}

func mapConfigTools(defs []config.ToolDefinition) []configquery.Tool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]configquery.Tool, 0, len(defs))
	for _, d := range defs {
		out = append(out, configquery.Tool{
			ID:       d.ID,
			Name:     d.Name,
			IconPath: d.IconPath,
			Type:     d.Type,
			URL:      d.URL,
			Action:   d.Action,
		})
	}
	return out
}
