package image

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

type ListAvailableImagesUseCaseInput struct {
}

type ListAvailableImagesUseCaseOutput struct {
	Images []domain.StaticImage
}

type ListAvailableImagesUseCase struct {
	repository staticImageRepository
}

func NewListAvailableImagesUseCase(repository staticImageRepository) *ListAvailableImagesUseCase {
	return &ListAvailableImagesUseCase{
		repository: repository,
	}
}

func (uc *ListAvailableImagesUseCase) Execute(ctx context.Context, input ListAvailableImagesUseCaseInput) (ListAvailableImagesUseCaseOutput, error) {
	images, err := uc.repository.FindAll(ctx)
	if err != nil {
		return ListAvailableImagesUseCaseOutput{}, err
	}

	return ListAvailableImagesUseCaseOutput{
		Images: images,
	}, nil
}
