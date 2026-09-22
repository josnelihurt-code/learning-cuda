package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

const maxFileSize = 10 * 1024 * 1024

var (
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
	if input.Filename == "" {
		return UploadImageUseCaseOutput{}, errEmptyFilename
	}

	if len(input.FileData) == 0 {
		return UploadImageUseCaseOutput{}, errEmptyFileData
	}

	if len(input.FileData) > maxFileSize {
		return UploadImageUseCaseOutput{}, ErrFileTooLarge
	}

	if !isPNGFormat(input.FileData) {
		return UploadImageUseCaseOutput{}, ErrInvalidFormat
	}

	image, err := uc.repository.Save(ctx, input.Filename, input.FileData)
	if err != nil {
		return UploadImageUseCaseOutput{}, fmt.Errorf("failed to save image: %w", err)
	}

	return UploadImageUseCaseOutput{Image: image}, nil
}

func isPNGFormat(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}
