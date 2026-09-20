#include "src/cpp_accelerator/application/engine/processor_engine.h"

#include <string>
#include <utility>

#include "src/cpp_accelerator/core/version.h"

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "src/cpp_accelerator/application/engine/filter_descriptor.h"
#include "src/cpp_accelerator/composition/platform/platform_support.h"
#include "src/cpp_accelerator/application/pipeline/filter_pipeline.h"
#include "src/cpp_accelerator/core/logger.h"
#include "src/cpp_accelerator/core/otel_metrics.h"
#include "src/cpp_accelerator/core/telemetry.h"
#include "src/cpp_accelerator/domain/interfaces/image_buffer.h"

namespace jrb::application::engine {

using jrb::domain::interfaces::GrayscaleAlgorithm;
using cuda_learning::BorderMode;
using cuda_learning::BORDER_MODE_CLAMP;
using cuda_learning::BORDER_MODE_WRAP;
using cuda_learning::InitRequest;
using cuda_learning::InitResponse;
using cuda_learning::ProcessImageRequest;
using cuda_learning::ProcessImageResponse;
using cuda_learning::GetCapabilitiesResponse;
using cuda_learning::AcceleratorType;
using cuda_learning::ACCELERATOR_TYPE_UNSPECIFIED;
using cuda_learning::GrayscaleType;
using cuda_learning::GRAYSCALE_TYPE_BT601;
using cuda_learning::GRAYSCALE_TYPE_BT709;
using cuda_learning::GRAYSCALE_TYPE_AVERAGE;
using cuda_learning::GRAYSCALE_TYPE_LIGHTNESS;
using cuda_learning::GRAYSCALE_TYPE_LUMINOSITY;
using cuda_learning::ACCELERATOR_TYPE_CUDA;
using cuda_learning::GRAYSCALE_TYPE_UNSPECIFIED;
using cuda_learning::ACCELERATOR_TYPE_UNSPECIFIED;
using cuda_learning::AcceleratorType_Name;
using cuda_learning::GrayscaleType_Name;
using cuda_learning::FILTER_TYPE_NONE;
using cuda_learning::FILTER_TYPE_GRAYSCALE;
using cuda_learning::FILTER_TYPE_BLUR;
using cuda_learning::FILTER_TYPE_MODEL_INFERENCE;

namespace {

BlurBorderMode ProtoToBlurBorderMode(BorderMode mode) {
  switch (mode) {
    case BORDER_MODE_CLAMP:
      return BlurBorderMode::CLAMP;
    case BORDER_MODE_WRAP:
      return BlurBorderMode::WRAP;
    default:
      return BlurBorderMode::REFLECT;
  }
}

}  // namespace

ProcessorEngine::ProcessorEngine(std::string component_name)
    : component_name_(std::move(component_name)) {
  RegisterPlatformAccelerators(factory_registry_);
}

bool ProcessorEngine::Initialize(const InitRequest& request,
                                  InitResponse* response) {
  static bool logger_initialized = false;
  if (!logger_initialized) {
    jrb::core::initialize_logger();
    jrb::core::otel::InitializeMetrics();
    auto logger = spdlog::default_logger();
    if (logger) {
      logger->flush();
    }
    logger_initialized = true;
  }

  if (!response) {
    return false;
  }

  spdlog::info("Initializing CUDA context (device: {})", request.cuda_device_id());

  try {
    auto& telemetry = jrb::core::telemetry::TelemetryManager::GetInstance();
    telemetry.Initialize("cuda-image-processor-cpp", "localhost:4317", true);

    response->set_code(0);
    response->set_message("CUDA context and telemetry initialized successfully");
    spdlog::info("Initialization successful (Telemetry)");

    InitializePlatformSubsystems(request, response);
  } catch (const std::exception& e) {
    spdlog::error("Initialization failed: {}", e.what());
    response->set_code(2);
    response->set_message(std::string("Initialization failed: ") + e.what());
    return false;
  }

  return true;
}

bool ProcessorEngine::ProcessImage(const ProcessImageRequest& request,
                                    ProcessImageResponse* response,
                                    void* memory_pool) {
  if (!response) {
    return false;
  }
  return ApplyFilters(request, response, memory_pool);
}

bool ProcessorEngine::GetCapabilities(GetCapabilitiesResponse* response,
                                       AcceleratorType requested_accelerator) {
  if (!response) {
    return false;
  }

  response->set_code(0);
  response->set_message("OK");

  auto* caps = response->mutable_capabilities();
  caps->set_api_version("2.1.0");
  caps->set_library_version(kLibraryVersionStr);
  caps->set_supports_streaming(false);
  caps->set_build_date(__DATE__);
  caps->set_build_commit(kLibraryGitHashStr);

  // When a specific accelerator is requested, return only that accelerator's filters.
  // When unspecified, return all factories' filters (backward compatibility).
  auto populate_from_factory = [&](IFilterFactory* factory) {
    if (!factory)
      return;
    for (const auto& fd : factory->GetFilterDescriptors()) {
      auto* filter = caps->add_filters();
      filter->set_id(fd.id);
      filter->set_name(fd.name);
      filter->add_supported_accelerators(factory->GetAcceleratorType());
      for (const auto& pd : fd.parameters) {
        auto* param = filter->add_parameters();
        param->set_id(pd.id);
        param->set_name(pd.name);
        param->set_type(pd.type);
        param->set_default_value(pd.default_value);
        for (const auto& opt : pd.options) {
          param->add_options(opt.value);
        }
        auto* meta = param->mutable_metadata();
        for (const auto& [k, v] : pd.metadata) {
          (*meta)[k] = v;
        }
      }
    }
  };

  if (requested_accelerator != ACCELERATOR_TYPE_UNSPECIFIED) {
    populate_from_factory(factory_registry_.GetFactory(requested_accelerator));
  } else {
    for (auto acc : factory_registry_.GetRegisteredTypes()) {
      populate_from_factory(factory_registry_.GetFactory(acc));
    }
  }

  return true;
}

GrayscaleAlgorithm ProcessorEngine::ProtoToAlgorithm(GrayscaleType type) const {
  switch (type) {
    case GRAYSCALE_TYPE_BT601:
      return GrayscaleAlgorithm::BT601;
    case GRAYSCALE_TYPE_BT709:
      return GrayscaleAlgorithm::BT709;
    case GRAYSCALE_TYPE_AVERAGE:
      return GrayscaleAlgorithm::Average;
    case GRAYSCALE_TYPE_LIGHTNESS:
      return GrayscaleAlgorithm::Lightness;
    case GRAYSCALE_TYPE_LUMINOSITY:
      return GrayscaleAlgorithm::Luminosity;
    default:
      return GrayscaleAlgorithm::BT601;
  }
}

bool ProcessorEngine::ApplyFilters(const ProcessImageRequest& request,
                                    ProcessImageResponse* response,
                                    void* memory_pool) {
  AcceleratorType accelerator = request.accelerator();
  if (accelerator == ACCELERATOR_TYPE_UNSPECIFIED) {
    accelerator = ACCELERATOR_TYPE_CUDA;
  }

  GrayscaleType grayscale_type = request.grayscale_type();
  if (grayscale_type == GRAYSCALE_TYPE_UNSPECIFIED) {
    grayscale_type = GRAYSCALE_TYPE_BT601;
  }

  auto& telemetry = jrb::core::telemetry::TelemetryManager::GetInstance();
  auto span =
      telemetry.CreateChildSpan(component_name_, "ProcessImage", request.trace_id(),
                                request.span_id(), static_cast<uint8_t>(request.trace_flags()));
  jrb::core::telemetry::ScopedSpan scoped_span(span);

  scoped_span.SetAttribute("image.width", static_cast<int64_t>(request.width()));
  scoped_span.SetAttribute("image.height", static_cast<int64_t>(request.height()));
  scoped_span.SetAttribute("image.channels", static_cast<int64_t>(request.channels()));
  scoped_span.SetAttribute("accelerator", AcceleratorType_Name(accelerator));
  scoped_span.SetAttribute("grayscale_type", GrayscaleType_Name(grayscale_type));
  scoped_span.SetAttribute("filters_count", static_cast<int64_t>(request.filters_size()));

  try {
    jrb::application::pipeline::FilterPipeline pipeline;
    std::string inference_model_id = "yolov10n";
    float inference_confidence = 0.5f;
    bool inference_requested = false;

    IFilterFactory* factory = factory_registry_.GetFactory(accelerator);

    for (int i = 0; i < request.filters_size(); i++) {
      const auto filter = request.filters(i);
      if (filter == FILTER_TYPE_NONE) {
        continue;
      }

      if (filter == FILTER_TYPE_GRAYSCALE) {
        FilterCreationParams params;
        params.grayscale_algorithm = ProtoToAlgorithm(grayscale_type);
        if (factory) {
          auto f = factory->CreateFilter(jrb::domain::interfaces::FilterType::GRAYSCALE, params);
          if (f) {
            scoped_span.AddEvent("Added grayscale filter to pipeline");
            pipeline.AddFilter(std::move(f));
          } else {
            spdlog::warn("Grayscale filter not supported by accelerator {}",
                         AcceleratorType_Name(accelerator));
          }
        }
      } else if (filter == FILTER_TYPE_BLUR) {
        FilterCreationParams params;
        if (request.has_blur_params()) {
          const auto& bp = request.blur_params();
          params.blur_kernel_size = bp.kernel_size() > 0 ? bp.kernel_size() : 5;
          params.blur_sigma = bp.sigma() > 0.0F ? bp.sigma() : 1.0F;
          params.blur_separable = bp.separable();
          params.blur_border_mode = ProtoToBlurBorderMode(bp.border_mode());
        }
        if (factory) {
          auto f = factory->CreateFilter(jrb::domain::interfaces::FilterType::BLUR, params);
          if (f) {
            scoped_span.AddEvent("Added blur filter to pipeline");
            pipeline.AddFilter(std::move(f));
          } else {
            spdlog::warn("Blur filter not supported by accelerator {}",
                         AcceleratorType_Name(accelerator));
          }
        }
      } else if (filter == FILTER_TYPE_MODEL_INFERENCE) {
        inference_requested = true;
        if (request.has_model_params()) {
          if (!request.model_params().model_id().empty()) {
            inference_model_id = request.model_params().model_id();
          }
          if (request.model_params().confidence_threshold() > 0) {
            inference_confidence = request.model_params().confidence_threshold();
          }
        }
        scoped_span.AddEvent("Model inference requested");
      } else {
        spdlog::warn("Unsupported filter type: {}", filter);
      }
    }

    if (pipeline.GetFilterCount() == 0 && !inference_requested) {
      spdlog::error("No valid filters to apply");
      response->set_code(5);
      response->set_message("No valid filters to apply");
      return false;
    }

    jrb::domain::interfaces::ImageBuffer input_buffer(
        reinterpret_cast<const unsigned char*>(request.image_data().data()), request.width(),
        request.height(), request.channels());

    int output_channels = request.channels();
    for (int i = 0; i < request.filters_size(); i++) {
      if (request.filters(i) == FILTER_TYPE_GRAYSCALE) {
        output_channels = 1;
        break;
      }
    }

    std::vector<unsigned char> output_data(request.width() * request.height() * output_channels);
    jrb::domain::interfaces::ImageBufferMut output_buffer(output_data.data(), request.width(),
                                                          request.height(), output_channels);

    bool pipeline_ran = false;
    if (pipeline.GetFilterCount() > 0) {
      bool success = pipeline.Apply(input_buffer, output_buffer, memory_pool);
      if (!success) {
        spdlog::error("Filter pipeline processing failed");
        scoped_span.RecordError("Filter pipeline processing failed");
        response->set_code(7);
        response->set_message("Filter pipeline processing failed");
        return false;
      }
      pipeline_ran = true;
    }

    if (inference_requested) {
      ApplyInference(inference_model_id, inference_confidence, pipeline_ran, request, response,
                     input_buffer, output_buffer);
    }

    response->set_code(0);
    response->set_message("Image processed successfully");
    response->set_image_data(output_data.data(), output_data.size());
    response->set_width(output_buffer.width);
    response->set_height(output_buffer.height);
    response->set_channels(output_buffer.channels);

    scoped_span.SetAttribute("detections.count", static_cast<int64_t>(response->detections_size()));

    scoped_span.SetAttribute("result.width", static_cast<int64_t>(output_buffer.width));
    scoped_span.SetAttribute("result.height", static_cast<int64_t>(output_buffer.height));
    scoped_span.SetAttribute("result.channels", static_cast<int64_t>(output_buffer.channels));

    return true;
  } catch (const std::exception& e) {
    spdlog::error("Exception during image processing: {}", e.what());
    scoped_span.RecordError(std::string("Exception: ") + e.what());
    response->set_code(6);
    response->set_message(std::string("Exception: ") + e.what());
    return false;
  }
}

}  // namespace jrb::application::engine
