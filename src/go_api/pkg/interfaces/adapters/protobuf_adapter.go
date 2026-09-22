package adapters

import (
	pb "github.com/jrb/cuda-learning/proto/gen"
	"github.com/jrb/cuda-learning/src/go_api/pkg/domain"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

type protobufAdapter struct{}

func newProtobufAdapter() *protobufAdapter {
	return &protobufAdapter{}
}

func (a *protobufAdapter) ToFilters(pbFilters []pb.FilterType) []domain.FilterType {
	filters := make([]domain.FilterType, 0, len(pbFilters))
	for _, f := range pbFilters {
		switch f {
		case pb.FilterType_FILTER_TYPE_GRAYSCALE:
			filters = append(filters, domain.FilterGrayscale)
		case pb.FilterType_FILTER_TYPE_BLUR:
			filters = append(filters, domain.FilterBlur)
		case pb.FilterType_FILTER_TYPE_MODEL_INFERENCE:
			filters = append(filters, domain.FilterModel)
		case pb.FilterType_FILTER_TYPE_NONE:
			filters = append(filters, domain.FilterNone)
		case pb.FilterType_FILTER_TYPE_UNSPECIFIED:
			logger.Global().Warn().Msg("received unspecified filter type, skipping")
		default:
			logger.Global().Warn().Int("filter_type", int(f)).Msg("received unknown filter type, skipping")
		}
	}
	return filters
}

func (a *protobufAdapter) ToAccelerator(pbAccel pb.AcceleratorType) domain.AcceleratorType {
	switch pbAccel {
	case pb.AcceleratorType_ACCELERATOR_TYPE_CUDA:
		return domain.AcceleratorCUDA
	case pb.AcceleratorType_ACCELERATOR_TYPE_CPU:
		return domain.AcceleratorCPU
	case pb.AcceleratorType_ACCELERATOR_TYPE_OPENCL:
		return domain.AcceleratorOpenCL
	case pb.AcceleratorType_ACCELERATOR_TYPE_VULKAN:
		return domain.AcceleratorVulkan
	case pb.AcceleratorType_ACCELERATOR_TYPE_UNSPECIFIED:
		logger.Global().Warn().Msg("received unspecified accelerator type, defaulting to CUDA")
		return domain.AcceleratorCUDA
	default:
		logger.Global().Warn().Int("accelerator_type", int(pbAccel)).Msg("received unknown accelerator type, defaulting to CUDA")
		return domain.AcceleratorCUDA
	}
}

func (a *protobufAdapter) ToGrayscaleType(pbType pb.GrayscaleType) domain.GrayscaleType {
	switch pbType {
	case pb.GrayscaleType_GRAYSCALE_TYPE_BT601:
		return domain.GrayscaleBT601
	case pb.GrayscaleType_GRAYSCALE_TYPE_BT709:
		return domain.GrayscaleBT709
	case pb.GrayscaleType_GRAYSCALE_TYPE_AVERAGE:
		return domain.GrayscaleAverage
	case pb.GrayscaleType_GRAYSCALE_TYPE_LIGHTNESS:
		return domain.GrayscaleLightness
	case pb.GrayscaleType_GRAYSCALE_TYPE_LUMINOSITY:
		return domain.GrayscaleLuminosity
	case pb.GrayscaleType_GRAYSCALE_TYPE_UNSPECIFIED:
		logger.Global().Warn().Msg("received unspecified grayscale type, defaulting to BT601")
		return domain.GrayscaleBT601
	default:
		logger.Global().Warn().Int("grayscale_type", int(pbType)).Msg("received unknown grayscale type, defaulting to BT601")
		return domain.GrayscaleBT601
	}
}

func (a *protobufAdapter) ToBorderMode(pbMode pb.BorderMode) domain.BorderMode {
	switch pbMode {
	case pb.BorderMode_BORDER_MODE_CLAMP:
		return domain.BorderModeClamp
	case pb.BorderMode_BORDER_MODE_REFLECT:
		return domain.BorderModeReflect
	case pb.BorderMode_BORDER_MODE_WRAP:
		return domain.BorderModeWrap
	case pb.BorderMode_BORDER_MODE_UNSPECIFIED:
		logger.Global().Warn().Msg("received unspecified border mode, defaulting to REFLECT")
		return domain.BorderModeReflect
	default:
		logger.Global().Warn().Int("border_mode", int(pbMode)).Msg("received unknown border mode, defaulting to REFLECT")
		return domain.BorderModeReflect
	}
}

func (a *protobufAdapter) ToBlurParameters(pbBlur *pb.GaussianBlurParameters) *domain.BlurParameters {
	if pbBlur == nil {
		return nil
	}

	return &domain.BlurParameters{
		KernelSize: pbBlur.KernelSize,
		Sigma:      pbBlur.Sigma,
		BorderMode: a.ToBorderMode(pbBlur.BorderMode),
		Separable:  pbBlur.Separable,
	}
}

func (a *protobufAdapter) ToProtobufBorderMode(mode domain.BorderMode) pb.BorderMode {
	switch mode {
	case domain.BorderModeClamp:
		return pb.BorderMode_BORDER_MODE_CLAMP
	case domain.BorderModeReflect:
		return pb.BorderMode_BORDER_MODE_REFLECT
	case domain.BorderModeWrap:
		return pb.BorderMode_BORDER_MODE_WRAP
	default:
		logger.Global().Warn().Str("border_mode", string(mode)).Msg("unknown border mode, defaulting to REFLECT")
		return pb.BorderMode_BORDER_MODE_REFLECT
	}
}

func (a *protobufAdapter) ToProtobufBlurParameters(blur *domain.BlurParameters) *pb.GaussianBlurParameters {
	if blur == nil {
		return nil
	}

	return &pb.GaussianBlurParameters{
		KernelSize: blur.KernelSize,
		Sigma:      blur.Sigma,
		BorderMode: a.ToProtobufBorderMode(blur.BorderMode),
		Separable:  blur.Separable,
	}
}
