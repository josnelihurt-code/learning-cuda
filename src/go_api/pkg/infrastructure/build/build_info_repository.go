package build

const (
	unknownValue = "unknown"
)

type infoRepositoryImpl struct {
	buildInfo *info
}

func NewBuildInfoRepository(buildInfo *info) *infoRepositoryImpl {
	return &infoRepositoryImpl{
		buildInfo: buildInfo,
	}
}

func (r *infoRepositoryImpl) GetVersion() string {
	if r.buildInfo == nil {
		return unknownValue
	}
	return r.buildInfo.Version
}

func (r *infoRepositoryImpl) GetBranch() string {
	if r.buildInfo == nil {
		return unknownValue
	}
	return r.buildInfo.Branch
}

func (r *infoRepositoryImpl) GetBuildTime() string {
	if r.buildInfo == nil {
		return unknownValue
	}
	return r.buildInfo.BuildTime
}

func (r *infoRepositoryImpl) GetCommitHash() string {
	if r.buildInfo == nil {
		return unknownValue
	}
	return r.buildInfo.CommitHash
}
