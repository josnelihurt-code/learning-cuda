package video

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	tracer := otel.Tracer("upload-video")
	ctx, span := tracer.Start(ctx, "UploadVideo",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("filename", input.Filename),
		attribute.Int("file_size", len(input.FileData)),
	)

	if err := uc.validateFormat(input.Filename); err != nil {
		span.SetAttributes(attribute.Bool("error.invalid_format", true))
		return UploadVideoUseCaseOutput{}, err
	}

	if err := uc.validateSize(input.FileData); err != nil {
		span.SetAttributes(attribute.Bool("error.file_too_large", true))
		return UploadVideoUseCaseOutput{}, err
	}

	id := strings.TrimSuffix(input.Filename, filepath.Ext(input.Filename))

	videoPath, err := uc.storage.Save(ctx, input.Filename, input.FileData)
	if err != nil {
		span.SetAttributes(attribute.Bool("error", true))
		return UploadVideoUseCaseOutput{}, fmt.Errorf("failed to save video: %w", err)
	}

	previewImagePath := ""
	if previewPath, err := uc.previews.Generate(ctx, id, videoPath); err != nil {
		log.FromContext(ctx).Warn().Err(err).Str("video_id", id).Msg("Failed to generate preview for uploaded video")
		span.AddEvent("preview_generation_failed")
		span.SetAttributes(attribute.String("preview.error", err.Error()))
	} else {
		previewImagePath = previewPath
		span.SetAttributes(attribute.Bool("preview.generated", true))
	}

	vid := &domain.Video{
		ID:               id,
		DisplayName:      strings.ReplaceAll(id, "-", " "),
		Path:             videoPath,
		PreviewImagePath: previewImagePath,
		IsDefault:        false,
	}

	if err := uc.repository.Save(ctx, vid); err != nil {
		span.SetAttributes(attribute.Bool("error", true))
		return UploadVideoUseCaseOutput{}, err
	}

	span.SetAttributes(
		attribute.String("video.id", vid.ID),
		attribute.Bool("upload.success", true),
	)

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
