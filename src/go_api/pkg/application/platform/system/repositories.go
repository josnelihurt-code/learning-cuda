package system

type configRepository interface {
	GetEnvironment() string
}

type buildInfoRepository interface {
	GetVersion() string
	GetBranch() string
	GetBuildTime() string
	GetCommitHash() string
}

type versionRepository interface {
	GetGoVersion() string
	GetProtoVersion() string
}
