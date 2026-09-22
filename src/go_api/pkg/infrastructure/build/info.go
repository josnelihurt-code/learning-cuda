package build

// info contains build-time information
type info struct {
	Version    string
	Branch     string
	BuildTime  string
	CommitHash string
}

// These variables can be set at build time using ldflags
var (
	version    = "1.0.0"
	branch     = "main"
	buildTime  = "unknown"
	commitHash = "unknown"
)

// NewBuildInfo creates a new info instance
// It reads from build-time ldflags
func NewBuildInfo() *info {
	return &info{
		Version:    version,
		Branch:     branch,
		BuildTime:  buildTime,
		CommitHash: commitHash,
	}
}
