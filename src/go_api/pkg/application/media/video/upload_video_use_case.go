package video

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
)

var (
	ErrInvalidFormat = errors.New("invalid format, only MP4 is supported")
	ErrFileTooLarge  = errors.New("file too large, maximum size is 100MB")
)

const maxVideoSize = 100 * 1024 * 1024

type UploadVideoUseCaseInput struct {
	FileData []byte
	Filename string
}

type UploadVideoUseCaseOutput struct {
	Video *domain.Video
}

type UploadVideoUseCase struct {
	repository videoRepository
	storage    videoStorage
	previews   previewGenerator
}

func NewUploadVideoUseCase(repository videoRepository, storage videoStorage, previews previewGenerator) *UploadVideoUseCase {
	return &UploadVideoUseCase{
		repository: repository,
		storage:    storage,
		previews:   previews,
	}
}

func (uc *UploadVideoUseCase) Execute(ctx context.Context, input UploadVideoUseCaseInput) (UploadVideoUseCaseOutput, error) {
	if err := uc.validateFormat(input.Filename); err != nil {
		return UploadVideoUseCaseOutput{}, err
	}

	if err := uc.validateSize(input.FileData); err != nil {
		return UploadVideoUseCaseOutput{}, err
	}

	id := strings.TrimSuffix(input.Filename, filepath.Ext(input.Filename))

	videoPath, err := uc.storage.Save(ctx, input.Filename, input.FileData)
	if err != nil {
		return UploadVideoUseCaseOutput{}, fmt.Errorf("failed to save video: %w", err)
	}

	previewImagePath := ""
	if previewPath, err := uc.previews.Generate(ctx, id, videoPath); err != nil {
		log.FromContext(ctx).Warn().Err(err).Str("video_id", id).Msg("Failed to generate preview for uploaded video")
	} else {
		previewImagePath = previewPath
	}

	vid := &domain.Video{
		ID:               id,
		DisplayName:      strings.ReplaceAll(id, "-", " "),
		Path:             videoPath,
		PreviewImagePath: previewImagePath,
		IsDefault:        false,
	}

	if err := uc.repository.Save(ctx, vid); err != nil {
		return UploadVideoUseCaseOutput{}, err
	}

	return UploadVideoUseCaseOutput{Video: vid}, nil
}

func (uc *UploadVideoUseCase) validateFormat(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".mp4" {
		return ErrInvalidFormat
	}
	return nil
}

func (uc *UploadVideoUseCase) validateSize(fileData []byte) error {
	if len(fileData) > maxVideoSize {
		return ErrFileTooLarge
	}
	return nil
}
