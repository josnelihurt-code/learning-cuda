package video

import (
	"context"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// FilePreviewGenerator generates preview images for stored videos via
// ffmpeg (see GeneratePreview). It implements the application layer's
// PreviewGenerator port: previews are written under diskDir and exposed
// under publicBase.
type FilePreviewGenerator struct {
	diskDir    string
	publicBase string
}

func NewFilePreviewGenerator(diskDir, publicBase string) *FilePreviewGenerator {
	return &FilePreviewGenerator{
		diskDir:    diskDir,
		publicBase: publicBase,
	}
}

func (g *FilePreviewGenerator) Generate(ctx context.Context, videoID, videoPath string) (string, error) {
	tracer := otel.Tracer("video-preview-generator")
	_, span := tracer.Start(ctx, "FilePreviewGenerator.Generate",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(attribute.String("video.id", videoID))

	// Public paths are served from the filesystem root, so the on-disk
	// location of a video is its public path without the leading slash.
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
