package domain

type FeatureFlag struct {
	Key          string
	Name         string
	Type         FeatureFlagType
	Enabled      bool
	DefaultValue any
	Description  string
}

type FeatureFlagType string

const (
	BooleanFlagType FeatureFlagType = "boolean"
	StringFlagType  FeatureFlagType = "string"
)

type FeatureFlagEvaluation struct {
	FlagKey      string
	EntityID     string
	Result       any
	Success      bool
	UsedFallback bool
}
