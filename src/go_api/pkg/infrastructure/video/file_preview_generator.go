package video

import (
	"context"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// FilePreviewGeneratorRepository generates preview images for stored
// videos via ffmpeg into diskDir, exposed under publicBase.
type FilePreviewGeneratorRepository struct {
	diskDir    string
	publicBase string
}

func NewFilePreviewGeneratorRepository(diskDir, publicBase string) *FilePreviewGeneratorRepository {
	return &FilePreviewGeneratorRepository{
		diskDir:    diskDir,
		publicBase: publicBase,
	}
}

func (g *FilePreviewGeneratorRepository) Generate(ctx context.Context, videoID, videoPath string) (string, error) {
	tracer := otel.Tracer("video-preview-generator")
	_, span := tracer.Start(ctx, "FilePreviewGeneratorRepository.Generate",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(attribute.String("video.id", videoID))

	// Public paths are served from the filesystem root, so the disk path drops the leading slash.
	diskVideoPath := strings.TrimPrefix(videoPath, "/")
	previewDiskPath := filepath.Join(g.diskDir, videoID+".png")

	if err := GeneratePreview(ctx, diskVideoPath, previewDiskPath); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "preview_generation_failed"))
		return "", err
	}

	publicPath := filepath.Join(g.publicBase, videoID+".png")

	span.SetAttributes(attribute.String("video.preview_path", publicPath))

	return publicPath, nil
}
