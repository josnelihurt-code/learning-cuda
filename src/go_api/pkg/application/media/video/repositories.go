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

type cameraSource interface {
	ListCameras(ctx context.Context) ([]domain.RemoteCamera, error)
}

// videoStorage persists uploaded video bytes and returns the
// public path under which the stored video is served.
type videoStorage interface {
	Save(ctx context.Context, filename string, data []byte) (videoPath string, err error)
}

// previewGenerator generates a preview image for a stored video
// and returns the public path of the generated preview.
type previewGenerator interface {
	Generate(ctx context.Context, videoID, videoPath string) (previewPath string, err error)
}
