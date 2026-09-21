package video

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	videoinfra "github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/video"
	"github.com/jrb/cuda-learning/src/go_api/pkg/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrInvalidFormat = errors.New("invalid format, only MP4 is supported")
	ErrFileTooLarge  = errors.New("file too large, maximum size is 100MB")
	rootPath         = "/"
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
	repository  videoRepository
	videosDir   string
	previewsDir string
}

func NewUploadVideoUseCase(repository videoRepository, videosDir, previewsDir string) *UploadVideoUseCase {
	return &UploadVideoUseCase{
		repository:  repository,
		videosDir:   videosDir,
		previewsDir: previewsDir,
	}
}

func (uc *UploadVideoUseCase) Execute(ctx context.Context, input UploadVideoUseCaseInput) (UploadVideoUseCaseOutput, error) {
	tracer := otel.Tracer("upload-video")
	_, span := tracer.Start(ctx, "UploadVideo",
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
	videoPath := filepath.Join(uc.videosDir, input.Filename)
	previewPath := filepath.Join(uc.previewsDir, id+".png")

	if err := os.WriteFile(videoPath, input.FileData, 0o600); err != nil {
		span.SetAttributes(attribute.Bool("error", true))
		return UploadVideoUseCaseOutput{}, fmt.Errorf("failed to save video: %w", err)
	}

	previewImagePath := ""
	if err := videoinfra.GeneratePreview(ctx, videoPath, previewPath); err != nil {
		log.FromContext(ctx).Warn().Err(err).Str("video_id", id).Msg("Failed to generate preview for uploaded video")
		span.AddEvent("preview_generation_failed")
		span.SetAttributes(attribute.String("preview.error", err.Error()))
	} else {
		previewImagePath = filepath.Join(rootPath, "data", "video_previews", id+".png")
		span.SetAttributes(attribute.Bool("preview.generated", true))
	}

	vid := &domain.Video{
		ID:               id,
		DisplayName:      strings.ReplaceAll(id, "-", " "),
		Path:             filepath.Join(rootPath, "data", "videos", input.Filename),
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
