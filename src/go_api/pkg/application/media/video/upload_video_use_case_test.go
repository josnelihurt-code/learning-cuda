package video

import (
	"context"
	"errors"
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fakevideoStorage struct {
	err        error
	savedName  string
	savedData  []byte
	publicPath string
}

func (f *fakevideoStorage) Save(_ context.Context, filename string, data []byte) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.savedName = filename
	f.savedData = data
	return f.publicPath, nil
}

type fakepreviewGenerator struct {
	err        error
	videoID    string
	videoPath  string
	publicPath string
}

func (f *fakepreviewGenerator) Generate(_ context.Context, videoID, videoPath string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.videoID = videoID
	f.videoPath = videoPath
	return f.publicPath, nil
}

func TestNewUploadVideoUseCase(t *testing.T) {
	// Arrange
	repo := new(MockVideoRepository)
	storage := &fakevideoStorage{}
	previews := &fakepreviewGenerator{}

	// Act
	sut := NewUploadVideoUseCase(repo, storage, previews)

	// Assert
	require.NotNil(t, sut)
	assert.Equal(t, repo, sut.repository)
	assert.Equal(t, storage, sut.storage)
	assert.Equal(t, previews, sut.previews)
}

func TestSuccess_UploadsVideoWithPathsFromPorts(t *testing.T) {
	// Arrange
	repo := new(MockVideoRepository)
	repo.On("Save", mock.Anything, mock.AnythingOfType("*domain.Video")).Return(nil).Once()
	storage := &fakevideoStorage{publicPath: "/data/videos/my-cool-video.mp4"}
	previews := &fakepreviewGenerator{publicPath: "/data/video_previews/my-cool-video.png"}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()
	data := []byte("fake video data")

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: data, Filename: "my-cool-video.mp4"})

	// Assert
	require.NoError(t, err)
	require.NotNil(t, output.Video)
	assert.Equal(t, "my-cool-video", output.Video.ID)
	assert.Equal(t, "my cool video", output.Video.DisplayName)
	assert.Equal(t, "/data/videos/my-cool-video.mp4", output.Video.Path)
	assert.Equal(t, "/data/video_previews/my-cool-video.png", output.Video.PreviewImagePath)
	assert.False(t, output.Video.IsDefault)

	assert.Equal(t, "my-cool-video.mp4", storage.savedName)
	assert.Equal(t, data, storage.savedData)
	assert.Equal(t, "my-cool-video", previews.videoID)
	assert.Equal(t, "/data/videos/my-cool-video.mp4", previews.videoPath)
	repo.AssertExpectations(t)
}

func TestSuccess_PreviewFailureStillUploadsVideo(t *testing.T) {
	// Arrange
	var saved *domain.Video
	repo := new(MockVideoRepository)
	repo.On("Save", mock.Anything, mock.AnythingOfType("*domain.Video")).Return(nil).Once().
		Run(func(args mock.Arguments) { saved = args.Get(1).(*domain.Video) })
	storage := &fakevideoStorage{publicPath: "/data/videos/test.mp4"}
	previews := &fakepreviewGenerator{err: errors.New("ffmpeg preview generation failed")}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: []byte("fake video"), Filename: "test.mp4"})

	// Assert
	require.NoError(t, err)
	require.NotNil(t, output.Video)
	assert.Equal(t, "/data/videos/test.mp4", output.Video.Path)
	assert.Empty(t, output.Video.PreviewImagePath)
	require.NotNil(t, saved)
	assert.Empty(t, saved.PreviewImagePath)
	repo.AssertExpectations(t)
}

func TestError_InvalidFormat(t *testing.T) {
	// Arrange
	repo := new(MockVideoRepository)
	storage := &fakevideoStorage{}
	previews := &fakepreviewGenerator{}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: []byte("fake video"), Filename: "test.avi"})

	// Assert
	assert.ErrorIs(t, err, ErrInvalidFormat)
	assert.Nil(t, output.Video)
	assert.Empty(t, storage.savedName, "storage port must not be called for invalid format")
	assert.Empty(t, previews.videoID, "preview port must not be called for invalid format")
}

func TestError_FileTooLarge(t *testing.T) {
	// Arrange
	repo := new(MockVideoRepository)
	storage := &fakevideoStorage{}
	previews := &fakepreviewGenerator{}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: make([]byte, maxVideoSize+1), Filename: "test.mp4"})

	// Assert
	assert.ErrorIs(t, err, ErrFileTooLarge)
	assert.Nil(t, output.Video)
	assert.Empty(t, storage.savedName, "storage port must not be called for oversized files")
	assert.Empty(t, previews.videoID, "preview port must not be called for oversized files")
}

func TestError_StorageSaveFails(t *testing.T) {
	// Arrange
	repo := new(MockVideoRepository)
	storage := &fakevideoStorage{err: errors.New("disk full")}
	previews := &fakepreviewGenerator{}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: []byte("fake video"), Filename: "test.mp4"})

	// Assert
	assert.ErrorContains(t, err, "failed to save video")
	assert.Nil(t, output.Video)
	assert.Empty(t, previews.videoID, "preview port must not be called when storage fails")
}

func TestError_RepositorySaveFails(t *testing.T) {
	// Arrange
	errSaveFailed := errors.New("save failed")
	repo := new(MockVideoRepository)
	repo.On("Save", mock.Anything, mock.AnythingOfType("*domain.Video")).Return(errSaveFailed).Once()
	storage := &fakevideoStorage{publicPath: "/data/videos/test.mp4"}
	previews := &fakepreviewGenerator{}
	sut := NewUploadVideoUseCase(repo, storage, previews)
	ctx := t.Context()

	// Act
	output, err := sut.Execute(ctx, UploadVideoUseCaseInput{FileData: []byte("fake video"), Filename: "test.mp4"})

	// Assert
	assert.ErrorIs(t, err, errSaveFailed)
	assert.Nil(t, output.Video)
	repo.AssertExpectations(t)
}

func TestUploadVideoUseCase_validateFormat(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		expectError bool
	}{
		{
			name:        "accepts MP4 files",
			filename:    "video.mp4",
			expectError: false,
		},
		{
			name:        "rejects AVI files",
			filename:    "video.avi",
			expectError: true,
		},
		{
			name:        "rejects MKV files",
			filename:    "video.mkv",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sut := &UploadVideoUseCase{}

			err := sut.validateFormat(tt.filename)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUploadVideoUseCase_validateSize(t *testing.T) {
	tests := []struct {
		name        string
		fileData    []byte
		expectError bool
	}{
		{
			name:        "accepts files under 100MB",
			fileData:    make([]byte, 50*1024*1024),
			expectError: false,
		},
		{
			name:        "rejects files over 100MB",
			fileData:    make([]byte, 101*1024*1024),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sut := &UploadVideoUseCase{}

			err := sut.validateSize(tt.fileData)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
