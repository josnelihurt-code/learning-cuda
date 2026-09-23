package system

import (
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockConfigRepository struct {
	mock.Mock
}

func (m *MockConfigRepository) GetEnvironment() string {
	args := m.Called()
	return args.String(0)
}

type MockBuildInfoRepository struct {
	mock.Mock
}

func (m *MockBuildInfoRepository) GetVersion() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockBuildInfoRepository) GetBranch() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockBuildInfoRepository) GetBuildTime() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockBuildInfoRepository) GetCommitHash() string {
	args := m.Called()
	return args.String(0)
}

type MockVersionRepository struct {
	mock.Mock
}

func (m *MockVersionRepository) GetGoVersion() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockVersionRepository) GetProtoVersion() string {
	args := m.Called()
	return args.String(0)
}

// assertSystemInfo compares the use case output against the expected values.
// CppVersion is intentionally unset here; the live source is accelerator
// registration, not a local VERSION file.
func assertSystemInfo(t *testing.T, result *domain.SystemInfo, goVersion, cppVersion, protoVersion, branch, buildTime, commitHash, environment string) {
	assert.Equal(t, goVersion, result.Version.GoVersion)
	assert.Equal(t, cppVersion, result.Version.CppVersion)
	assert.Equal(t, protoVersion, result.Version.ProtoVersion)
	assert.Equal(t, branch, result.Version.Branch)
	assert.Equal(t, buildTime, result.Version.BuildTime)
	assert.Equal(t, commitHash, result.Version.CommitHash)
	assert.Equal(t, environment, result.Environment)
}

func TestNewGetSystemInfoUseCase(t *testing.T) {
	// Arrange
	mockConfig := &MockConfigRepository{}
	mockBuildInfo := &MockBuildInfoRepository{}
	mockVersion := &MockVersionRepository{}

	// Act
	sut := NewGetSystemInfoUseCase(mockConfig, mockBuildInfo, mockVersion)

	// Assert
	require.NotNil(t, sut)
	assert.Equal(t, mockConfig, sut.configRepo)
	assert.Equal(t, mockBuildInfo, sut.buildInfoRepo)
	assert.Equal(t, mockVersion, sut.versionRepo)
}

func TestGetSystemInfoUseCase_Execute(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func(*MockConfigRepository, *MockBuildInfoRepository, *MockVersionRepository)
		assertResult func(t *testing.T, result *domain.SystemInfo, err error)
	}{
		{
			name: "Success_AllFieldsPopulated",
			setupMocks: func(config *MockConfigRepository, buildInfo *MockBuildInfoRepository, version *MockVersionRepository) {
				config.On("GetEnvironment").Return("development")
				buildInfo.On("GetBranch").Return("main")
				buildInfo.On("GetBuildTime").Return("2024-10-25T12:00:00Z")
				buildInfo.On("GetCommitHash").Return("abc123")
				version.On("GetGoVersion").Return("1.0.8")
				version.On("GetProtoVersion").Return("1.0.0")
			},
			assertResult: func(t *testing.T, result *domain.SystemInfo, err error) {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assertSystemInfo(t, result, "1.0.8", "", "1.0.0", "main", "2024-10-25T12:00:00Z", "abc123", "development")
			},
		},
		{
			name: "Success_ProductionEnvironment",
			setupMocks: func(config *MockConfigRepository, buildInfo *MockBuildInfoRepository, version *MockVersionRepository) {
				config.On("GetEnvironment").Return("production")
				buildInfo.On("GetBranch").Return("main")
				buildInfo.On("GetBuildTime").Return("2024-10-25T12:00:00Z")
				buildInfo.On("GetCommitHash").Return("abc123")
				version.On("GetGoVersion").Return("1.0.8")
				version.On("GetProtoVersion").Return("1.0.0")
			},
			assertResult: func(t *testing.T, result *domain.SystemInfo, err error) {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assertSystemInfo(t, result, "1.0.8", "", "1.0.0", "main", "2024-10-25T12:00:00Z", "abc123", "production")
			},
		},
		{
			name: "Success_CustomValues",
			setupMocks: func(config *MockConfigRepository, buildInfo *MockBuildInfoRepository, version *MockVersionRepository) {
				config.On("GetEnvironment").Return("staging")
				buildInfo.On("GetBranch").Return("develop")
				buildInfo.On("GetBuildTime").Return("2024-11-01T10:30:00Z")
				buildInfo.On("GetCommitHash").Return("xyz789")
				version.On("GetGoVersion").Return("2.0.0")
				version.On("GetProtoVersion").Return("2.0.0")
			},
			assertResult: func(t *testing.T, result *domain.SystemInfo, err error) {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assertSystemInfo(t, result, "2.0.0", "", "2.0.0", "develop", "2024-11-01T10:30:00Z", "xyz789", "staging")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockConfig := &MockConfigRepository{}
			mockBuildInfo := &MockBuildInfoRepository{}
			mockVersion := &MockVersionRepository{}

			tt.setupMocks(mockConfig, mockBuildInfo, mockVersion)

			sut := NewGetSystemInfoUseCase(mockConfig, mockBuildInfo, mockVersion)
			ctx := t.Context()

			// Act
			output, err := sut.Execute(ctx, GetSystemInfoUseCaseInput{})

			// Assert
			tt.assertResult(t, output.SystemInfo, err)
			mockConfig.AssertExpectations(t)
			mockBuildInfo.AssertExpectations(t)
			mockVersion.AssertExpectations(t)
		})
	}
}
