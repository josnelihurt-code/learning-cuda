package connectrpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/src/go_api/pkg/application"
	imageapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/image"
	videoapp "github.com/jrb/cuda-learning/src/go_api/pkg/application/media/video"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type FileHandler struct {
	listAvailableImagesUseCase application.UseCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput]
	uploadImageUseCase         application.UseCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput]
	listAvailableVideosUseCase application.UseCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput]
	uploadVideoUseCase         application.UseCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput]
}

func NewFileHandler(
	listAvailableImagesUC application.UseCase[imageapp.ListAvailableImagesUseCaseInput, imageapp.ListAvailableImagesUseCaseOutput],
	uploadImageUC application.UseCase[imageapp.UploadImageUseCaseInput, imageapp.UploadImageUseCaseOutput],
	listAvailableVideosUC application.UseCase[videoapp.ListVideosUseCaseInput, videoapp.ListVideosUseCaseOutput],
	uploadVideoUC application.UseCase[videoapp.UploadVideoUseCaseInput, videoapp.UploadVideoUseCaseOutput],
) *FileHandler {
	return &FileHandler{
		listAvailableImagesUseCase: listAvailableImagesUC,
		uploadImageUseCase:         uploadImageUC,
		listAvailableVideosUseCase: listAvailableVideosUC,
		uploadVideoUseCase:         uploadVideoUC,
	}
}

func (h *FileHandler) ListAvailableImages(
	ctx context.Context,
	req *connect.Request[pb.ListAvailableImagesRequest],
) (*connect.Response[pb.ListAvailableImagesResponse], error) {
	span := trace.SpanFromContext(ctx)

	result, err := h.listAvailableImagesUseCase.Execute(ctx, imageapp.ListAvailableImagesUseCaseInput{})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to list available images")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbImages := make([]*pb.StaticImage, 0, len(result.Images))
	for _, img := range result.Images {
		pbImages = append(pbImages, &pb.StaticImage{
			Id:          img.ID,
			DisplayName: img.DisplayName,
			Path:        img.Path,
			IsDefault:   img.IsDefault,
		})
	}

	span.SetAttributes(
		attribute.Int("available_images.count", len(pbImages)),
	)

	return connect.NewResponse(&pb.ListAvailableImagesResponse{
		Images: pbImages,
	}), nil
}

func (h *FileHandler) UploadImage(
	ctx context.Context,
	req *connect.Request[pb.UploadImageRequest],
) (*connect.Response[pb.UploadImageResponse], error) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("filename", req.Msg.Filename),
		attribute.Int("file_size", len(req.Msg.FileData)),
	)

	result, err := h.uploadImageUseCase.Execute(ctx, imageapp.UploadImageUseCaseInput{
		Filename: req.Msg.Filename,
		FileData: req.Msg.FileData,
	})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to upload image")

		code := connect.CodeInternal
		if errors.Is(err, imageapp.ErrFileTooLarge) || errors.Is(err, imageapp.ErrInvalidFormat) {
			code = connect.CodeInvalidArgument
		}

		return nil, connect.NewError(code, err)
	}

	span.SetAttributes(
		attribute.String("image.id", result.Image.ID),
		attribute.String("image.path", result.Image.Path),
	)

	logger.FromContext(ctx).Info().Str("image_id", result.Image.ID).Msg("Image uploaded successfully")

	return connect.NewResponse(&pb.UploadImageResponse{
		Image: &pb.StaticImage{
			Id:          result.Image.ID,
			DisplayName: result.Image.DisplayName,
			Path:        result.Image.Path,
			IsDefault:   result.Image.IsDefault,
		},
		Message: "Image uploaded successfully",
	}), nil
}

func (h *FileHandler) ListAvailableVideos(
	ctx context.Context,
	req *connect.Request[pb.ListAvailableVideosRequest],
) (*connect.Response[pb.ListAvailableVideosResponse], error) {
	span := trace.SpanFromContext(ctx)

	result, err := h.listAvailableVideosUseCase.Execute(ctx, videoapp.ListVideosUseCaseInput{})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to list available videos")
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbVideos := make([]*pb.StaticVideo, 0, len(result.Videos))
	for _, vid := range result.Videos {
		pbVideos = append(pbVideos, &pb.StaticVideo{
			Id:               vid.ID,
			DisplayName:      vid.DisplayName,
			Path:             vid.Path,
			PreviewImagePath: vid.PreviewImagePath,
			IsDefault:        vid.IsDefault,
		})
	}

	span.SetAttributes(
		attribute.Int("available_videos.count", len(pbVideos)),
	)

	return connect.NewResponse(&pb.ListAvailableVideosResponse{
		Videos: pbVideos,
	}), nil
}

func (h *FileHandler) UploadVideo(
	ctx context.Context,
	req *connect.Request[pb.UploadVideoRequest],
) (*connect.Response[pb.UploadVideoResponse], error) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("filename", req.Msg.Filename),
		attribute.Int("file_size", len(req.Msg.FileData)),
	)

	result, err := h.uploadVideoUseCase.Execute(ctx, videoapp.UploadVideoUseCaseInput{
		FileData: req.Msg.FileData,
		Filename: req.Msg.Filename,
	})
	if err != nil {
		span.RecordError(err)
		logger.FromContext(ctx).Error().Err(err).Msg("Failed to upload video")

		code := connect.CodeInternal
		if errors.Is(err, videoapp.ErrFileTooLarge) || errors.Is(err, videoapp.ErrInvalidFormat) {
			code = connect.CodeInvalidArgument
		}

		return nil, connect.NewError(code, err)
	}

	span.SetAttributes(
		attribute.String("video.id", result.Video.ID),
		attribute.String("video.path", result.Video.Path),
	)

	logger.FromContext(ctx).Info().Str("video_id", result.Video.ID).Msg("Video uploaded successfully")

	return connect.NewResponse(&pb.UploadVideoResponse{
		Video: &pb.StaticVideo{
			Id:               result.Video.ID,
			DisplayName:      result.Video.DisplayName,
			Path:             result.Video.Path,
			PreviewImagePath: result.Video.PreviewImagePath,
			IsDefault:        result.Video.IsDefault,
		},
		Message: "Video uploaded successfully",
	}), nil
}
