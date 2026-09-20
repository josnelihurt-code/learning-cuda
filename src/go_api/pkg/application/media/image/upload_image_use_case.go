package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const maxFileSize = 10 * 1024 * 1024

var (
	// ErrFileTooLarge and ErrInvalidFormat are matched by the connect handler
	// to map validation failures to connect error codes.
	ErrFileTooLarge  = errors.New("file too large")
	ErrInvalidFormat = errors.New("invalid format")
	errEmptyFilename = errors.New("empty filename")
	errEmptyFileData = errors.New("empty file data")
)

// pngSignature is the 8-byte PNG magic number.
var pngSignature = []byte{137, 80, 78, 71, 13, 10, 26, 10}

type UploadImageUseCaseInput struct {
	Filename string
	FileData []byte
}

type UploadImageUseCaseOutput struct {
	Image *domain.StaticImage
}

type UploadImageUseCase struct {
	repository staticImageRepository
}

func NewUploadImageUseCase(repository staticImageRepository) *UploadImageUseCase {
	return &UploadImageUseCase{
		repository: repository,
	}
}

func (uc *UploadImageUseCase) Execute(ctx context.Context, input UploadImageUseCaseInput) (UploadImageUseCaseOutput, error) {
	tracer := otel.Tracer("upload-image")
	ctx, span := tracer.Start(ctx, "UploadImageUseCase.Execute",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("filename", input.Filename),
		attribute.Int("file_size", len(input.FileData)),
	)

	if input.Filename == "" {
		span.RecordError(errEmptyFilename)
		return UploadImageUseCaseOutput{}, errEmptyFilename
	}

	if len(input.FileData) == 0 {
		span.RecordError(errEmptyFileData)
		return UploadImageUseCaseOutput{}, errEmptyFileData
	}

	if len(input.FileData) > maxFileSize {
		span.RecordError(ErrFileTooLarge)
		span.SetAttributes(attribute.String("validation.error", "file_too_large"))
		return UploadImageUseCaseOutput{}, ErrFileTooLarge
	}

	if !isPNGFormat(input.FileData) {
		span.RecordError(ErrInvalidFormat)
		span.SetAttributes(attribute.String("validation.error", "invalid_format"))
		return UploadImageUseCaseOutput{}, ErrInvalidFormat
	}

	image, err := uc.repository.Save(ctx, input.Filename, input.FileData)
	if err != nil {
		span.RecordError(err)
		return UploadImageUseCaseOutput{}, fmt.Errorf("failed to save image: %w", err)
	}

	span.SetAttributes(
		attribute.String("image.id", image.ID),
		attribute.String("image.path", image.Path),
	)

	return UploadImageUseCaseOutput{Image: image}, nil
}

func isPNGFormat(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}
