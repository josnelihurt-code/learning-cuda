package image

import (
	"context"
	"testing"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockStaticImageRepository struct {
	mock.Mock
}

func (m *MockStaticImageRepository) FindAll(ctx context.Context) ([]domain.StaticImage, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.StaticImage), args.Error(1)
}

func (m *MockStaticImageRepository) Save(ctx context.Context, filename string, data []byte) (*domain.StaticImage, error) {
	args := m.Called(ctx, filename, data)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.StaticImage), args.Error(1)
}

func TestNewListAvailableImagesUseCase(t *testing.T) {
	mockRepo := new(MockStaticImageRepository)
	sut := NewListAvailableImagesUseCase(mockRepo)

	assert.NotNil(t, sut)
	assert.Equal(t, mockRepo, sut.repository)
}

func TestListAvailableImagesUseCase_Execute(t *testing.T) {
	tests := []struct {
		name         string
		setupMock    func(*MockStaticImageRepository)
		assertResult func(t *testing.T, result []domain.StaticImage, err error)
	}{
		{
			name: "returns empty list when no images found",
			setupMock: func(m *MockStaticImageRepository) {
				m.On("FindAll", mock.Anything).Return([]domain.StaticImage{}, nil)
			},
			assertResult: func(t *testing.T, result []domain.StaticImage, err error) {
				assert.NoError(t, err)
				assert.Empty(t, result)
			},
		},
		{
			name: "returns list of images from repository",
			setupMock: func(m *MockStaticImageRepository) {
				images := []domain.StaticImage{
					{ID: "lena", DisplayName: "Lena", Path: "/data/static_images/lena.png", IsDefault: true},
					{ID: "mandrill", DisplayName: "Mandrill", Path: "/data/static_images/mandrill.png", IsDefault: false},
					{ID: "peppers", DisplayName: "Peppers", Path: "/data/static_images/peppers.png", IsDefault: false},
				}
				m.On("FindAll", mock.Anything).Return(images, nil)
			},
			assertResult: func(t *testing.T, result []domain.StaticImage, err error) {
				assert.NoError(t, err)
				require.Len(t, result, 3)

				imageMap := make(map[string]domain.StaticImage)
				for _, img := range result {
					imageMap[img.ID] = img
				}

				lena, exists := imageMap["lena"]
				require.True(t, exists)
				assert.Equal(t, "Lena", lena.DisplayName)
				assert.True(t, lena.IsDefault)
				assert.Contains(t, lena.Path, "lena.png")

				mandrill, exists := imageMap["mandrill"]
				require.True(t, exists)
				assert.Equal(t, "Mandrill", mandrill.DisplayName)
				assert.False(t, mandrill.IsDefault)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockStaticImageRepository)
			tt.setupMock(mockRepo)

			sut := NewListAvailableImagesUseCase(mockRepo)
			ctx := t.Context()

			output, err := sut.Execute(ctx, ListAvailableImagesUseCaseInput{})

			tt.assertResult(t, output.Images, err)
			mockRepo.AssertExpectations(t)
		})
	}
}
