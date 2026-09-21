package video

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
)

type videoRepository interface {
	List(ctx context.Context) ([]domain.Video, error)
	GetByID(ctx context.Context, id string) (*domain.Video, error)
	Save(ctx context.Context, video *domain.Video) error
}

type cameraRepository interface {
	ListCameras(ctx context.Context) ([]domain.RemoteCamera, error)
}

// VideoStorage persists uploaded video bytes and returns the public path
// under which the stored video is served. It is a consumer-owned port:
// the application layer defines it and infrastructure implements it, so
// storage details (directories, permissions, public path mapping) stay
// out of the use cases.
type VideoStorage interface {
	Save(ctx context.Context, filename string, data []byte) (videoPath string, err error)
}

// PreviewGenerator generates a preview image for an already-stored video —
// identified by its ID and public path — and returns the public path of
// the generated preview. It is a consumer-owned port: the application
// layer defines it and infrastructure implements it.
type PreviewGenerator interface {
	Generate(ctx context.Context, videoID, videoPath string) (previewPath string, err error)
}
