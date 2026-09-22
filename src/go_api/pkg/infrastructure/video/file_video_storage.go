package video

import (
	"context"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// FileVideoStorage stores uploaded videos under diskDir with
// 0o600 permissions and returns their public path under publicBase.
type FileVideoStorage struct {
	diskDir    string
	publicBase string
}

func NewFileVideoStorage(diskDir, publicBase string) *FileVideoStorage {
	return &FileVideoStorage{
		diskDir:    diskDir,
		publicBase: publicBase,
	}
}

func (s *FileVideoStorage) Save(ctx context.Context, filename string, data []byte) (string, error) {
	tracer := otel.Tracer("video-storage")
	_, span := tracer.Start(ctx, "FileVideoStorage.Save",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("filename", filename),
		attribute.Int("file_size", len(data)),
	)

	diskPath := filepath.Join(s.diskDir, filename)
	if err := os.WriteFile(diskPath, data, 0o600); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error.type", "file_write_failed"))
		return "", err
	}

	publicPath := filepath.Join(s.publicBase, filename)

	span.SetAttributes(attribute.String("video.path", publicPath))

	return publicPath, nil
}
