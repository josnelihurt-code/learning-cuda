package video

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

type ListVideosUseCaseInput struct{}

type ListVideosUseCaseOutput struct {
	Videos []domain.Video
}

type ListVideosUseCase struct {
	repository videoRepository
}

func NewListVideosUseCase(repository videoRepository) *ListVideosUseCase {
	return &ListVideosUseCase{
		repository: repository,
	}
}

func (uc *ListVideosUseCase) Execute(ctx context.Context, _ ListVideosUseCaseInput) (ListVideosUseCaseOutput, error) {
	videos, err := uc.repository.List(ctx)
	if err != nil {
		return ListVideosUseCaseOutput{}, err
	}

	return ListVideosUseCaseOutput{Videos: videos}, nil
}
