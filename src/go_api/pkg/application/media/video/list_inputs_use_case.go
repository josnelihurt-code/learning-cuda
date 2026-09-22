package video

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type InputSource struct {
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
	Inputs []InputSource
}

type ListInputsUseCase struct {
	videoRepository  videoRepository
	cameraSource cameraSource
}

func NewListInputsUseCase(videoRepository videoRepository, cameraSource cameraSource) *ListInputsUseCase {
	return &ListInputsUseCase{
		videoRepository:  videoRepository,
		cameraSource: cameraSource,
	}
}

func (uc *ListInputsUseCase) Execute(ctx context.Context, _ ListInputsUseCaseInput) (ListInputsUseCaseOutput, error) {
	tracer := otel.Tracer("list-inputs")
	_, span := tracer.Start(ctx, "ListInputs",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	sources := []InputSource{
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
			sources = append(sources, InputSource{
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
				sources = append(sources, InputSource{
					ID:          fmt.Sprintf("remote-camera-%d", cam.SensorID),
					DisplayName: cam.DisplayName,
					Type:        "remote_camera",
					SensorID:    cam.SensorID,
					IsDefault:   false,
				})
			}
		}
	}

	staticCount := 0
	cameraCount := 0
	videoCount := 0
	remoteCameraCount := 0
	for _, src := range sources {
		switch src.Type {
		case "static":
			staticCount++
		case "camera":
			cameraCount++
		case "video":
			videoCount++
		case "remote_camera":
			remoteCameraCount++
		}
	}

	span.SetAttributes(
		attribute.Int("input_sources.count", len(sources)),
		attribute.Int("input_sources.static_count", staticCount),
		attribute.Int("input_sources.camera_count", cameraCount),
		attribute.Int("input_sources.video_count", videoCount),
		attribute.Int("input_sources.remote_camera_count", remoteCameraCount),
	)

	return ListInputsUseCaseOutput{Inputs: sources}, nil
}
