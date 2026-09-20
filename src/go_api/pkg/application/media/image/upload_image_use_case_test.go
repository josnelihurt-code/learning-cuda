package image

import (
	"context"
	"errors"
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockStaticImageRepository struct {
	mock.Mock
}

func (m *mockStaticImageRepository) FindAll(ctx context.Context) ([]domain.StaticImage, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.StaticImage), args.Error(1)
}

func (m *mockStaticImageRepository) Save(ctx context.Context, filename string, data []byte) (*domain.StaticImage, error) {
	args := m.Called(ctx, filename, data)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.StaticImage), args.Error(1)
}

func makeValidPNGData() []byte {
	return []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13}
}

func makeLargePNGData() []byte {
	data := make([]byte, 11*1024*1024)
	copy(data, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	return data
}

func makeInvalidFormatData() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0}
}

func makeValidImage() *domain.StaticImage {
	return &domain.StaticImage{
		ID:          "test-upload",
		DisplayName: "Test Upload",
		Path:        "/data/static_images/test-upload.png",
		IsDefault:   false,
	}
}

func TestNewUploadImageUseCase(t *testing.T) {
	// Arrange
	repo := new(mockStaticImageRepository)

	// Act
	sut := NewUploadImageUseCase(repo)

	// Assert
	require.NotNil(t, sut)
	assert.Equal(t, repo, sut.repository)
}

func TestUploadImageUseCase_Execute(t *testing.T) {
	var (
		errSaveFailed = errors.New("save failed")
	)

	tests := []struct {
		name         string
		filename     string
		fileData     []byte
		mockImage    *domain.StaticImage
		mockError    error
		assertResult func(t *testing.T, image *domain.StaticImage, err error)
	}{
		{
			name:      "Success_ValidPNGImage",
			filename:  "test-upload.png",
			fileData:  makeValidPNGData(),
			mockImage: makeValidImage(),
			mockError: nil,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.NoError(t, err)
				require.NotNil(t, image)
				assert.Equal(t, "test-upload", image.ID)
				assert.Equal(t, "Test Upload", image.DisplayName)
				assert.False(t, image.IsDefault)
			},
		},
		{
			name:      "Error_FileTooLarge",
			filename:  "large.png",
			fileData:  makeLargePNGData(),
			mockImage: nil,
			mockError: nil,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.ErrorIs(t, err, ErrFileTooLarge)
				assert.Nil(t, image)
			},
		},
		{
			name:      "Error_InvalidFormat",
			filename:  "test.jpg",
			fileData:  makeInvalidFormatData(),
			mockImage: nil,
			mockError: nil,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.ErrorIs(t, err, ErrInvalidFormat)
				assert.Nil(t, image)
			},
		},
		{
			name:      "Error_EmptyFilename",
			filename:  "",
			fileData:  makeValidPNGData(),
			mockImage: nil,
			mockError: nil,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.ErrorIs(t, err, errEmptyFilename)
				assert.Nil(t, image)
			},
		},
		{
			name:      "Error_EmptyFileData",
			filename:  "test.png",
			fileData:  []byte{},
			mockImage: nil,
			mockError: nil,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.ErrorIs(t, err, errEmptyFileData)
				assert.Nil(t, image)
			},
		},
		{
			name:      "Error_RepositorySaveFailed",
			filename:  "test.png",
			fileData:  makeValidPNGData(),
			mockImage: nil,
			mockError: errSaveFailed,
			assertResult: func(t *testing.T, image *domain.StaticImage, err error) {
				assert.Error(t, err)
				assert.Nil(t, image)
				assert.Contains(t, err.Error(), "failed to save image")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := new(mockStaticImageRepository)
			if tt.mockImage != nil || tt.mockError != nil {
				mockRepo.On("Save",
					mock.Anything,
					tt.filename,
					tt.fileData,
				).Return(tt.mockImage, tt.mockError).Once()
			}

			sut := NewUploadImageUseCase(mockRepo)
			ctx := t.Context()

			// Act
			output, err := sut.Execute(ctx, UploadImageUseCaseInput{FileData: tt.fileData, Filename: tt.filename})

			// Assert
			tt.assertResult(t, output.Image, err)
			if tt.mockImage != nil || tt.mockError != nil {
				mockRepo.AssertExpectations(t)
			}
		})
	}
}

func TestIsPNGFormat(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "Success_ValidPNG",
			data:     []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13},
			expected: true,
		},
		{
			name:     "Error_InvalidHeader",
			data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
			expected: false,
		},
		{
			name:     "Error_TooShort",
			data:     []byte{137, 80, 78},
			expected: false,
		},
		{
			name:     "Error_Empty",
			data:     []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result := isPNGFormat(tt.data)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}
