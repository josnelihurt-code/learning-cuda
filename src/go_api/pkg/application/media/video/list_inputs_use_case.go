package video

import (
	"context"
	"fmt"
)

type inputSource struct {
	ID               string
	DisplayName      string
	Type             string
	ImagePath        string
	IsDefault        bool
	VideoPath        string
	PreviewImagePath string
	SensorID         int32
}

type ListInputsUseCaseInput struct{}

type ListInputsUseCaseOutput struct {
	Inputs []inputSource
}

type listInputsUseCase struct {
	videoRepository videoRepository
	cameraSource    cameraSource
}

func NewListInputsUseCase(videoRepository videoRepository, cameraSource cameraSource) *listInputsUseCase {
	return &listInputsUseCase{
		videoRepository: videoRepository,
		cameraSource:    cameraSource,
	}
}

func (uc *listInputsUseCase) Execute(ctx context.Context, _ ListInputsUseCaseInput) (ListInputsUseCaseOutput, error) {
	sources := []inputSource{
		{
			ID:          "gallery",
			DisplayName: "Gallery",
			Type:        "static",
			ImagePath:   "/data/static_images/lena.png",
			IsDefault:   false,
		},
		{
			ID:          "webcam",
			DisplayName: "Camera",
			Type:        "camera",
			ImagePath:   "",
			IsDefault:   true,
		},
	}

	videos, err := uc.videoRepository.List(ctx)
	if err == nil {
		for _, vid := range videos {
			sources = append(sources, inputSource{
				ID:               vid.ID,
				DisplayName:      vid.DisplayName,
				Type:             "video",
				VideoPath:        vid.Path,
				PreviewImagePath: vid.PreviewImagePath,
				IsDefault:        vid.IsDefault,
			})
		}
	}

	if uc.cameraSource != nil {
		cameras, camErr := uc.cameraSource.ListCameras(ctx)
		if camErr == nil {
			for _, cam := range cameras {
				sources = append(sources, inputSource{
					ID:          fmt.Sprintf("remote-camera-%d", cam.SensorID),
					DisplayName: cam.DisplayName,
					Type:        "remote_camera",
					SensorID:    cam.SensorID,
					IsDefault:   false,
				})
			}
		}
	}

	return ListInputsUseCaseOutput{Inputs: sources}, nil
}
