import { createPromiseClient, type PromiseClient } from '@connectrpc/connect';
import { context, propagation } from '@opentelemetry/api';
import type { IFrameTransportService } from '@/domain/interfaces/i-frame-transport-service';
import { AcceleratorConfig, FilterData, GrayscaleAlgorithm, ImageData } from '@/domain/value-objects';
import { VideoPlaybackService } from '@/gen/image_processor_service_connect';
import {
  DetectionFrame,
  GenericFilterParameterSelection,
  GenericFilterSelection,
  ProcessingStatsFrame,
  ProcessImageRequest,
  ProcessImageResponse,
  StartVideoPlaybackRequest,
  StopVideoPlaybackRequest,
} from '@/gen/image_processor_service_pb';
import { BorderMode, GrayscaleType, ModelInferenceParameters, TraceContext } from '@/gen/common_pb';
import type { IStatsDisplay, IToastDisplay, ICameraPreview } from './transport-types';
import { ChunkReassembler, nextMessageId, packMessage } from './data-channel-framing';
import { webrtcService } from '@/infrastructure/connection/webrtc-service';
import { createGrpcConnectTransport } from '@/infrastructure/grpc/create-grpc-transport';
import { logger } from '@/infrastructure/observability/otel-logger';

type FrameResultCallback = (data: ProcessImageResponse) => void;

const REQUEST_TIMEOUT_MS = 10_000;
const DEFAULT_THRESHOLD = "0.45";

type PendingResponse = {
  resolve: (response: ProcessImageResponse) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

function serializeValue(value: unknown): string {
  if (value === undefined || value === null) {
    return '';
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false';
  }
  return String(value);
}

function toGenericFilterSelections(filters: FilterData[]): GenericFilterSelection[] {
  return filters
    .filter((filter) => !filter.isNone())
    .map((filter) => {
      const parameters = Object.entries(filter.getParameters())
        .map(([parameterId, value]) => {
          const values = Array.isArray(value)
            ? value.map((entry) => serializeValue(entry)).filter((entry) => entry !== '')
            : [serializeValue(value)].filter((entry) => entry !== '');
          if (values.length === 0) {
            return null;
          }
          return new GenericFilterParameterSelection({
            parameterId,
            values,
          });
        })
        .filter((selection): selection is GenericFilterParameterSelection => selection !== null);

      return new GenericFilterSelection({
        filterId: filter.getId(),
        parameters,
      });
    });
}

function extractBlurParams(filters: FilterData[]) {
  const blur = filters.find((filter) => filter.isBlur());
  if (!blur) {
    return undefined;
  }

  const params = blur.getParameters();
  const kernelValue = typeof params.kernel_size === 'string' ? parseInt(params.kernel_size, 10) : params.kernel_size;
  let kernelSize = typeof kernelValue === 'number' && !Number.isNaN(kernelValue) && kernelValue > 0 ? kernelValue : 5;
  if (kernelSize % 2 === 0) {
    kernelSize += 1;
  }

  const sigmaValue = typeof params.sigma === 'string' ? parseFloat(params.sigma) : params.sigma;
  const sigma = typeof sigmaValue === 'number' && !Number.isNaN(sigmaValue) && sigmaValue >= 0 ? sigmaValue : 1;
  const borderModeValue = String(params.border_mode ?? 'REFLECT').toUpperCase();
  const borderMode = borderModeValue === 'CLAMP'
    ? BorderMode.CLAMP
    : borderModeValue === 'WRAP'
      ? BorderMode.WRAP
      : BorderMode.REFLECT;
  const separable = typeof params.separable === 'string'
    ? params.separable === 'true' || params.separable === '1'
    : params.separable === undefined
      ? true
      : Boolean(params.separable);

  return { kernelSize, sigma, borderMode, separable };
}

function buildTraceContext(): TraceContext {
  const carrier: Record<string, string> = {};
  propagation.inject(context.active(), carrier);
  return new TraceContext({
    traceparent: carrier.traceparent ?? '',
    tracestate: carrier.tracestate ?? '',
  });
}

async function rasterizeImageData(
  dataUrl: string,
  width: number,
  height: number
): Promise<{ imageData: Uint8Array; channels: number }> {
  const image = new Image();
  image.crossOrigin = 'anonymous';

  await new Promise<void>((resolve, reject) => {
    image.onload = () => resolve();
    image.onerror = () => reject(new Error('Failed to load frame image data'));
    image.src = dataUrl;
  });

  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;

  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (!context) {
    throw new Error('Canvas context is not available');
  }

  context.drawImage(image, 0, 0, width, height);
  const raster = context.getImageData(0, 0, width, height);
  const rgb = new Uint8Array(width * height * 3);

  for (let index = 0, pixel = 0; index < raster.data.length; index += 4, pixel += 3) {
    rgb[pixel] = raster.data[index];
    rgb[pixel + 1] = raster.data[index + 1];
    rgb[pixel + 2] = raster.data[index + 2];
  }

  return {
    imageData: rgb,
    channels: 3,
  };
}

export class WebRTCFrameTransportService implements IFrameTransportService {
  private readonly client: PromiseClient<typeof VideoPlaybackService>;
  private sessionId: string | null = null;
  private frameResultCallback: FrameResultCallback | null = null;
  private detectionResultCallback: ((frame: DetectionFrame) => void) | null = null;
  private statsResultCallback: ((frame: ProcessingStatsFrame) => void) | null = null;
  private frameIdCounter = 0n;
  private connectPromise: Promise<void> | null = null;
  private pendingResponses: Map<bigint, PendingResponse> = new Map();
  private connectionState: 'connected' | 'disconnected' | 'connecting' | 'error' = 'disconnected';
  private frameChannelReassembler = new ChunkReassembler();
  private detectionChannelReassembler = new ChunkReassembler();
  private statsChannelReassembler = new ChunkReassembler();
  private lastRequest: string | null = null;
  private lastRequestTime: Date | null = null;

  constructor(
    private sourceId: string,
    private statsManager: IStatsDisplay,
    private cameraManager: ICameraPreview,
    private toastManager: IToastDisplay
  ) {
    this.client = createPromiseClient(VideoPlaybackService, createGrpcConnectTransport());
  }

  connect(): void {
    void this.ensureConnected();
  }

  disconnect(): void {
    const sessionId = this.sessionId;
    this.sessionId = null;
    this.connectPromise = null;
    this.connectionState = 'disconnected';
    const pending = Array.from(this.pendingResponses.values());
    this.pendingResponses.clear();
    for (const entry of pending) {
      clearTimeout(entry.timer);
      entry.reject(new Error('Transport disconnected'));
    }
    this.statsManager.updateTransportStatus('disconnected', 'Disconnected');

    if (sessionId) {
      void webrtcService.closeSession(sessionId).catch((error) => {
        logger.error('Failed to close frame transport session', {
          'error.message': error instanceof Error ? error.message : String(error),
          'webrtc.session_id': sessionId,
        });
      });
    }
  }

  sendFrame(
    imageData: string,
    width: number,
    height: number,
    filters: FilterData[],
    accelerator: string
  ): void {
    this.sendFrameWithImageData(new ImageData(imageData, width, height), filters, accelerator);
  }

  sendFrameWithImageData(image: ImageData, filters: FilterData[], accelerator: string): void {
    this.sendFrameWithValueObjects(image, filters, accelerator);
  }

  sendFrameWithValueObjects(image: ImageData, filters: FilterData[], accelerator: string): void {
    this.sendFrameWithProcessingConfig(
      image,
      filters,
      new AcceleratorConfig(accelerator),
      new GrayscaleAlgorithm('bt601')
    );
  }

  sendFrameWithProcessingConfig(
    image: ImageData,
    filters: FilterData[],
    accelerator: AcceleratorConfig,
    grayscale: GrayscaleAlgorithm
  ): void {
    void this.sendRequest(image, filters, accelerator, grayscale, { awaitResponse: false }).catch((error) => {
      this.handleError('Failed to send frame', error);
    });
  }

  sendSingleFrame(
    imageData: string,
    width: number,
    height: number,
    filters: FilterData[],
    accelerator: string
  ): Promise<ProcessImageResponse> {
    const image = new ImageData(imageData, width, height);
    const acceleratorConfig = new AcceleratorConfig(accelerator);
    const grayscale = new GrayscaleAlgorithm('bt601');

    return this.sendRequest(image, filters, acceleratorConfig, grayscale, { awaitResponse: true }) as Promise<ProcessImageResponse>;
  }

  async sendSingleImage(
    rasterizedBytes: Uint8Array,
    width: number,
    height: number,
    channels: number,
    filters: FilterData[],
    accelerator: AcceleratorConfig,
    grayscale: GrayscaleAlgorithm
  ): Promise<ProcessImageResponse> {
    await this.ensureConnected();
    if (!this.sessionId) {
      throw new Error('WebRTC session is not available');
    }

    const dataChannel = webrtcService.getDataChannel(this.sessionId);
    if (!dataChannel || dataChannel.readyState !== 'open') {
      throw new Error('WebRTC data channel is not open');
    }

    this.lastRequest = 'ProcessImageRequest';
    this.lastRequestTime = new Date();

    const frameId = ++this.frameIdCounter;
    const pending = this.registerPending(frameId);

    const modelFilter = filters.find((f) => f.getId() === 'model_inference');
    const modelParams = modelFilter
      ? new ModelInferenceParameters({
        modelId: modelFilter.getParameter('model_id') || 'yolov10n',
        confidenceThreshold: parseFloat(modelFilter.getParameter('confidence_threshold') ?? DEFAULT_THRESHOLD),
      })
      : undefined;

    const payload = new ProcessImageRequest({
      imageData: rasterizedBytes,
      width,
      height,
      channels,
      filters: filters.filter((filter) => !filter.isNone()).map((filter) => filter.toProtocol()),
      accelerator: accelerator.toProtocol(),
      grayscaleType: filters.some((filter) => filter.isGrayscale()) ? grayscale.toProtocol() : GrayscaleType.UNSPECIFIED,
      blurParams: extractBlurParams(filters),
      genericFilters: toGenericFilterSelections(filters),
      modelParams,
      sessionId: this.sessionId,
      traceContext: buildTraceContext(),
      apiVersion: '1.1',
      frameId,
    }).toBinary();

    try {
      for (const chunk of packMessage(nextMessageId(), payload)) {
        dataChannel.send(chunk);
      }
    } catch (error) {
      this.rejectPending(frameId, error instanceof Error ? error : new Error(String(error)));
      throw error;
    }

    return pending;
  }

  sendStartVideo(videoId: string, filters: FilterData[], accelerator: string): void {
    const acceleratorConfig = new AcceleratorConfig(accelerator);
    const grayscale = new GrayscaleAlgorithm('bt601');
    const modelFilter = filters.find((f) => f.getId() === 'model_inference');
    const modelParams = modelFilter
      ? new ModelInferenceParameters({
        modelId: modelFilter.getParameter('model_id') || 'yolov10n',
        confidenceThreshold: parseFloat(modelFilter.getParameter('confidence_threshold') ?? DEFAULT_THRESHOLD),
      })
      : undefined;

    void this.ensureConnected()
      .then(() => this.client.startVideoPlayback(new StartVideoPlaybackRequest({
        videoId,
        sessionId: this.sessionId ?? '',
        filters: filters.filter((filter) => !filter.isNone()).map((filter) => filter.toProtocol()),
        accelerator: acceleratorConfig.toProtocol(),
        grayscaleType: filters.some((filter) => filter.isGrayscale()) ? grayscale.toProtocol() : GrayscaleType.UNSPECIFIED,
        blurParams: extractBlurParams(filters),
        genericFilters: toGenericFilterSelections(filters),
        modelParams,
        traceContext: buildTraceContext(),
        apiVersion: '1.1',
      })))
      .then(() => {
        this.lastRequest = 'StartVideoPlayback';
        this.lastRequestTime = new Date();
      })
      .catch((error) => {
        this.handleError('Failed to start video playback', error);
      });
  }

  sendStopVideo(videoId?: string): void {
    void videoId;
    if (!this.sessionId) {
      return;
    }

    void this.client.stopVideoPlayback(new StopVideoPlaybackRequest({
      sessionId: this.sessionId,
      traceContext: buildTraceContext(),
      apiVersion: '1.1',
    })).then(() => {
      this.lastRequest = 'StopVideoPlayback';
      this.lastRequestTime = new Date();
    }).catch((error) => {
      this.handleError('Failed to stop video playback', error);
    });
  }

  onFrameResult(callback: (data: ProcessImageResponse) => void): void {
    this.frameResultCallback = callback;
  }

  onDetectionResult(callback: (frame: DetectionFrame) => void): void {
    this.detectionResultCallback = callback;
  }

  onStatsResult(callback: (frame: ProcessingStatsFrame) => void): void {
    this.statsResultCallback = callback;
  }

  isConnected(): boolean {
    if (!this.sessionId) {
      return false;
    }
    return webrtcService.getDataChannel(this.sessionId)?.readyState === 'open';
  }

  getConnectionStatus(): { state: 'connected' | 'disconnected' | 'connecting' | 'error'; lastRequest: string | null; lastRequestTime: Date | null } {
    return {
      state: this.connectionState,
      lastRequest: this.lastRequest,
      lastRequestTime: this.lastRequestTime,
    };
  }

  private async ensureConnected(): Promise<void> {
    if (this.isConnected()) {
      return;
    }
    if (this.connectPromise) {
      return this.connectPromise;
    }

    this.connectionState = 'connecting';
    this.statsManager.updateTransportStatus('connecting', 'Connecting...');

    this.connectPromise = (async () => {
      const session = await webrtcService.createSession(this.sourceId);
      this.sessionId = session.getId();
      // waitForTransportReady waits until readyState === 'open' AND the
      // underlying SCTP association is 'connected'. Without the second wait
      // the first send() races the SCTP handshake and Chrome tears the
      // channel down with OperationError: data-channel-failure.
      const dataChannel = await webrtcService.waitForTransportReady(session.getId());
      webrtcService.ensureStatsDataChannel(session.getId());
      this.attachDataChannel(dataChannel);
      this.attachDetectionChannel(session.getId());
      this.attachStatsChannel(session.getId());
      this.connectionState = 'connected';
      this.lastRequest = 'Data channel connected';
      this.lastRequestTime = new Date();
      this.statsManager.updateTransportStatus('connected', 'Connected');
    })().finally(() => {
      this.connectPromise = null;
    });

    return this.connectPromise;
  }

  private attachDataChannel(dataChannel: RTCDataChannel): void {
    dataChannel.binaryType = 'arraybuffer';
    this.frameChannelReassembler = new ChunkReassembler();
    const previousOnMessage = dataChannel.onmessage;
    const previousOnError = dataChannel.onerror;
    const previousOnClose = dataChannel.onclose;

    dataChannel.onmessage = (event: MessageEvent<ArrayBuffer | Blob | string>) => {
      previousOnMessage?.call(dataChannel, event);
      const payload = event.data;
      if (typeof payload === 'string') {
        return;
      }
      const bufferPromise = payload instanceof Blob ? payload.arrayBuffer() : Promise.resolve(payload);
      void bufferPromise.then((buffer) => {
        const assembled = this.frameChannelReassembler.pushChunk(buffer);
        if (assembled === null) {
          return;
        }
        this.handleResponse(ProcessImageResponse.fromBinary(assembled));
      }).catch((error) => {
        this.handleError('Failed to decode frame response', error);
      });
    };
    dataChannel.onerror = (event) => {
      previousOnError?.call(dataChannel, event);
      this.connectionState = 'error';
      this.statsManager.updateTransportStatus('disconnected', 'Transport error');
    };
    dataChannel.onclose = (event: Event) => {
      previousOnClose?.call(dataChannel, event);
      this.connectionState = 'disconnected';
      this.statsManager.updateTransportStatus('disconnected', 'Disconnected');
      this.failPendingResponses(new Error('WebRTC data channel closed'));
    };
  }

  private failPendingResponses(error: Error): void {
    if (this.pendingResponses.size === 0) {
      return;
    }
    const pending = Array.from(this.pendingResponses.entries());
    this.pendingResponses.clear();
    for (const [, entry] of pending) {
      clearTimeout(entry.timer);
      entry.reject(error);
    }
  }

  private attachDetectionChannel(sessionId: string): void {
    const dc = webrtcService.getDetectionDataChannel(sessionId);
    if (!dc) return;
    this.detectionChannelReassembler = new ChunkReassembler();
    const previousOnMessage = dc.onmessage;
    dc.onmessage = (event: MessageEvent) => {
      previousOnMessage?.call(dc, event);
      const payload = event.data;
      const bufferPromise = payload instanceof Blob ? payload.arrayBuffer() : Promise.resolve(payload as ArrayBuffer);
      void bufferPromise.then((buffer) => {
        const assembled = this.detectionChannelReassembler.pushChunk(buffer);
        if (assembled === null) {
          return;
        }
        const frame = DetectionFrame.fromBinary(assembled);
        this.detectionResultCallback?.(frame);
      }).catch((error) => {
        this.handleError('Failed to decode detection frame', error);
      });
    };
  }

  private attachStatsChannel(sessionId: string): void {
    const dc = webrtcService.getStatsDataChannel(sessionId);
    if (!dc) return;
    this.statsChannelReassembler = new ChunkReassembler();
    dc.onmessage = (event: MessageEvent) => {
      const payload = event.data;
      const bufferPromise = payload instanceof Blob ? payload.arrayBuffer() : Promise.resolve(payload as ArrayBuffer);
      void bufferPromise.then((buffer) => {
        const assembled = this.statsChannelReassembler.pushChunk(buffer);
        if (assembled === null) {
          return;
        }
        const frame = ProcessingStatsFrame.fromBinary(assembled);
        this.statsResultCallback?.(frame);
      }).catch((error) => {
        this.handleError('Failed to decode processing stats frame', error);
      });
    };
  }

  private async sendRequest(
    image: ImageData,
    filters: FilterData[],
    accelerator: AcceleratorConfig,
    grayscale: GrayscaleAlgorithm,
    options: { awaitResponse: boolean }
  ): Promise<ProcessImageResponse | void> {
    await this.ensureConnected();
    if (!this.sessionId) {
      throw new Error('WebRTC session is not available');
    }

    const dataChannel = webrtcService.getDataChannel(this.sessionId);
    if (!dataChannel || dataChannel.readyState !== 'open') {
      throw new Error('WebRTC data channel is not open');
    }

    this.cameraManager.setProcessing(true);
    this.lastRequest = 'ProcessImageRequest';
    this.lastRequestTime = new Date();

    const rasterized = await rasterizeImageData(
      image.getBase64(),
      image.getWidth(),
      image.getHeight()
    );

    const frameId = ++this.frameIdCounter;
    const pending = options.awaitResponse ? this.registerPending(frameId) : null;

    const modelFilter = filters.find((f) => f.getId() === 'model_inference');
    const modelParams = modelFilter
      ? new ModelInferenceParameters({
        modelId: modelFilter.getParameter('model_id') || 'yolov10n',
        confidenceThreshold: parseFloat(modelFilter.getParameter('confidence_threshold') ?? DEFAULT_THRESHOLD),
      })
      : undefined;

    const payload = new ProcessImageRequest({
      imageData: rasterized.imageData,
      width: image.getWidth(),
      height: image.getHeight(),
      channels: rasterized.channels,
      filters: filters.filter((filter) => !filter.isNone()).map((filter) => filter.toProtocol()),
      accelerator: accelerator.toProtocol(),
      grayscaleType: filters.some((filter) => filter.isGrayscale()) ? grayscale.toProtocol() : GrayscaleType.UNSPECIFIED,
      blurParams: extractBlurParams(filters),
      genericFilters: toGenericFilterSelections(filters),
      modelParams,
      sessionId: this.sessionId,
      traceContext: buildTraceContext(),
      apiVersion: '1.1',
      frameId,
    }).toBinary();

    try {
      for (const chunk of packMessage(nextMessageId(), payload)) {
        dataChannel.send(chunk);
      }
    } catch (error) {
      if (pending) {
        this.rejectPending(frameId, error instanceof Error ? error : new Error(String(error)));
      }
      throw error;
    }

    if (pending) {
      return pending;
    }
    return undefined;
  }

  private registerPending(frameId: bigint): Promise<ProcessImageResponse> {
    return new Promise<ProcessImageResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingResponses.delete(frameId);
        reject(new Error(`Frame ${frameId} timed out after ${REQUEST_TIMEOUT_MS}ms`));
      }, REQUEST_TIMEOUT_MS);
      this.pendingResponses.set(frameId, { resolve, reject, timer });
    });
  }

  private rejectPending(frameId: bigint, error: Error): void {
    const entry = this.pendingResponses.get(frameId);
    if (!entry) {
      return;
    }
    clearTimeout(entry.timer);
    this.pendingResponses.delete(frameId);
    entry.reject(error);
  }

  private handleResponse(response: ProcessImageResponse): void {
    this.cameraManager.setProcessing(false);
    this.connectionState = 'connected';
    this.lastRequest = 'ProcessImageResponse';
    this.lastRequestTime = new Date();

    if (response.code === 0) {
      this.statsManager.updateProcessingStats(performance.now() - this.cameraManager.getLastFrameTime());
    } else {
      this.toastManager.error('Processing Error', response.message || 'Unknown error');
      this.statsManager.updateCameraStatus('Processing failed', 'error');
    }

    const frameId = response.frameId;
    if (frameId && frameId !== 0n) {
      const pending = this.pendingResponses.get(frameId);
      if (pending) {
        clearTimeout(pending.timer);
        this.pendingResponses.delete(frameId);
        if (response.code === 0) {
          pending.resolve(response);
        } else {
          pending.reject(new Error(response.message || 'Frame processing failed'));
        }
      }
    }

    this.frameResultCallback?.(response);

    if (this.detectionResultCallback && response.detections && response.detections.length > 0) {
      const synthesized = new DetectionFrame({
        frameId: response.frameId,
        imageWidth: response.width,
        imageHeight: response.height,
        detections: response.detections,
      });
      this.detectionResultCallback(synthesized);
    }
  }

  private handleError(message: string, error: unknown): void {
    const normalized = error instanceof Error ? error : new Error(String(error));
    this.connectionState = 'error';
    this.cameraManager.setProcessing(false);
    this.statsManager.updateTransportStatus('disconnected', 'Connection error');
    logger.error(message, {
      'error.message': normalized.message,
      'webrtc.source_id': this.sourceId,
    });
    this.toastManager.warning('Transport Error', normalized.message);
  }
}
