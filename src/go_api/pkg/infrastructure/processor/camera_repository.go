package processor

import (
	"context"

	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/rs/zerolog/log"
)

// RegistryCameraRepository implements video.cameraRepository by reading the
// Cameras list from the first registered accelerator session.
type RegistryCameraRepository struct {
	registry *Registry
}

func NewRegistryCameraRepository(registry *Registry) *RegistryCameraRepository {
	return &RegistryCameraRepository{registry: registry}
}

func (r *RegistryCameraRepository) ListCameras(ctx context.Context) ([]domain.RemoteCamera, error) {
	sess, ok := r.registry.First()
	if !ok || sess == nil {
		log.Debug().Msg("ListCameras: no accelerator session registered")
		return nil, nil
	}
	log.Debug().Int("camera_count", len(sess.Cameras)).Str("device_id", sess.DeviceID).Msg("ListCameras: session found")
	result := make([]domain.RemoteCamera, 0, len(sess.Cameras))
	for _, cam := range sess.Cameras {
		if cam == nil {
			continue
		}
		log.Debug().Int32("sensor_id", cam.SensorId).Str("display_name", cam.DisplayName).Msg("ListCameras: camera entry")
		result = append(result, domain.RemoteCamera{
			SensorID:    cam.SensorId,
			DisplayName: cam.DisplayName,
			Model:       cam.Model,
		})
	}
	return result, nil
}
