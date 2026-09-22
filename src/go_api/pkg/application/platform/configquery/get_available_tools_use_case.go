package configquery

import "context"

type GetAvailableToolsUseCaseInput struct{}

type GetAvailableToolsUseCaseOutput struct {
	Environment string
	Categories  []ToolCategory
}

type getAvailableToolsUseCase struct {
	catalog toolsCatalog
}

func NewGetAvailableToolsUseCase(catalog toolsCatalog) *getAvailableToolsUseCase {
	return &getAvailableToolsUseCase{catalog: catalog}
}

func (uc *getAvailableToolsUseCase) Execute(
	ctx context.Context,
	_ GetAvailableToolsUseCaseInput,
) (GetAvailableToolsUseCaseOutput, error) {
	_ = ctx
	categories := make([]ToolCategory, 0, 3)
	if tools := uc.catalog.ObservabilityTools(); len(tools) > 0 {
		categories = append(categories, ToolCategory{ID: "observability", Name: "Observability", Tools: tools})
	}
	if tools := uc.catalog.FeaturesTools(); len(tools) > 0 {
		categories = append(categories, ToolCategory{ID: "features", Name: "Features", Tools: tools})
	}
	if tools := uc.catalog.TestingTools(); len(tools) > 0 {
		categories = append(categories, ToolCategory{ID: "testing", Name: "Testing", Tools: tools})
	}
	return GetAvailableToolsUseCaseOutput{
		Environment: uc.catalog.Environment(),
		Categories:  categories,
	}, nil
}
