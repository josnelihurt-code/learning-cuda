import { useCallback, useRef } from 'react';
import type { Dispatch, RefObject } from 'react';
import type { ActiveFilterState } from '@/presentation/components/filters/FilterPanel';
import type { IToastDisplay } from '@/infrastructure/transport/transport-types';
import type { GridSource, GridSourceAction } from '@/presentation/utils/grid-source';
import { GridSourceActionType } from '@/presentation/utils/grid-source';
import { hasModelInferenceFilter } from '@/presentation/utils/grid-source';
import { statsFrameToMetrics } from '@/presentation/utils/metric-point';
import { webrtcService } from '@/infrastructure/connection/webrtc-service';
import { logger } from '@/infrastructure/observability/otel-logger';
import { ChunkReassembler } from '@/infrastructure/transport/data-channel-framing';
import { AcceleratorType } from '@/gen/common_pb';
import {
  DetectionFrame,
  GenericFilterParameterSelection,
  GenericFilterSelection,
  ProcessingStatsFrame,
  ProcessImageRequest,
} from '@/gen/image_processor_service_pb';

function toProtocolAccelerator(value: 'gpu' | 'cpu'): AcceleratorType {
  return value === 'cpu' ? AcceleratorType.CPU : AcceleratorType.CUDA;
}

function filtersToGenericSelections(filters: ActiveFilterState[]): GenericFilterSelection[] {
  const nonNoneFilters = filters.filter((f) => f.id !== 'none');
  if (nonNoneFilters.length === 0) {
    return [new GenericFilterSelection({ filterId: 'none', parameters: [] })];
  }
  return nonNoneFilters.map(
    (filter) =>
      new GenericFilterSelection({
        filterId: filter.id,
        parameters: Object.entries(filter.parameters).map(
          ([parameterId, value]) =>
            new GenericFilterParameterSelection({ parameterId, values: [value] })
        ),
      })
  );
}

type CameraTransportOptions = {
  dispatch: Dispatch<GridSourceAction>;
  sourcesRef: RefObject<GridSource[]>;
  toastManager: IToastDisplay;
};

type CameraTransportResult = {
  createCameraSession: (sourceId: string, stream: MediaStream) => Promise<void>;
  replaceCameraStream: (sourceId: string, stream: MediaStream) => Promise<void>;
  sendCameraControlRequest: (
    sessionId: string,
    sourceId: string,
    filters: ActiveFilterState[],
    accelerator: 'gpu' | 'cpu'
  ) => void;
};

export function useCameraTransport({
  dispatch,
  sourcesRef,
  toastManager,
}: CameraTransportOptions): CameraTransportResult {
  const cameraSessionSourceIdsRef = useRef<Set<string>>(new Set());

  const sendCameraControlRequest = useCallback(
    (sessionId: string, sourceId: string, filters: ActiveFilterState[], accelerator: 'gpu' | 'cpu'): void => {
      try {
        webrtcService.sendControlRequest(
          sessionId,
          new ProcessImageRequest({
            sessionId,
            genericFilters: filtersToGenericSelections(filters),
            accelerator: toProtocolAccelerator(accelerator),
            apiVersion: '1.1',
          })
        );
      } catch (error) {
        logger.error('Failed to send live camera filter update', {
          'error.message': error instanceof Error ? error.message : String(error),
          'source.id': sourceId,
          'session.id': sessionId,
        });
      }
    },
    []
  );

  const createCameraSession = useCallback(
    async (sourceId: string, stream: MediaStream): Promise<void> => {
      if (cameraSessionSourceIdsRef.current.has(sourceId)) {
        return;
      }
      const source = sourcesRef.current.find((item) => item.id === sourceId);
      if (!source || source.sessionId) {
        return;
      }

      cameraSessionSourceIdsRef.current.add(sourceId);
      try {
        const session = await webrtcService.createSession(sourceId, {
          mode: 'camera-mediatrack',
          localStream: stream,
          useDataChannel: true,
          onRemoteStream: (remoteStream) => {
            dispatch({ type: GridSourceActionType.SET_REMOTE_STREAM, payload: { sourceId, remoteStream } });
          },
        });

        dispatch({
          type: GridSourceActionType.SET_SESSION,
          payload: {
            sourceId,
            sessionId: session.getId(),
            sessionMode: session.getMode() as 'frame-processing' | 'camera-mediatrack',
          },
        });

        const currentSource = sourcesRef.current.find((item) => item.id === sourceId) ?? source;
        const currentFilters =
          currentSource.filters.length > 0
            ? currentSource.filters
            : [{ id: 'none', parameters: {} }];
        sendCameraControlRequest(
          session.getId(),
          sourceId,
          currentFilters,
          currentSource.accelerator
        );

        const detectionChannel = webrtcService.getDetectionDataChannel(session.getId());
        if (detectionChannel) {
          const reassembler = new ChunkReassembler();
          detectionChannel.onmessage = (event: MessageEvent): void => {
            const payload = event.data as ArrayBuffer | Blob;
            const bufferPromise =
              payload instanceof Blob ? payload.arrayBuffer() : Promise.resolve(payload as ArrayBuffer);
            void bufferPromise.then((buffer) => {
              const assembled = reassembler.pushChunk(buffer);
              if (assembled === null) return;
              const frame = DetectionFrame.fromBinary(assembled);
              const activeSource = sourcesRef.current.find((item) => item.id === sourceId);
              const shouldShowDetections = activeSource
                ? hasModelInferenceFilter(activeSource.filters)
                : false;
              if (!shouldShowDetections) {
                dispatch({
                  type: GridSourceActionType.SET_DETECTIONS,
                  payload: {
                    sourceId,
                    detections: [],
                    width: 0,
                    height: 0,
                  },
                });
                return;
              }
              dispatch({
                type: GridSourceActionType.SET_DETECTIONS,
                payload: {
                  sourceId,
                  detections: frame.detections,
                  width: frame.imageWidth,
                  height: frame.imageHeight,
                },
              });
            });
          };
        }

        const statsChannel =
          webrtcService.ensureStatsDataChannel(session.getId()) ??
          webrtcService.getStatsDataChannel(session.getId());
        if (statsChannel) {
          const statsReassembler = new ChunkReassembler();
          statsChannel.onmessage = (event: MessageEvent): void => {
            const payload = event.data as ArrayBuffer | Blob;
            const bufferPromise =
              payload instanceof Blob ? payload.arrayBuffer() : Promise.resolve(payload as ArrayBuffer);
            void bufferPromise.then((buffer) => {
              const assembled = statsReassembler.pushChunk(buffer);
              if (assembled === null) return;
              const statsFrame = ProcessingStatsFrame.fromBinary(assembled);
              const metrics = statsFrameToMetrics(statsFrame);
              if (!Object.keys(metrics).length) return;

              dispatch({
                type: GridSourceActionType.SET_SOURCE_METRICS,
                payload: { sourceId, metrics },
              });
            });
          };
        }
      } catch (error) {
        logger.error('Failed to create camera MediaTrack session', {
          'error.message': error instanceof Error ? error.message : String(error),
          'source.id': sourceId,
        });
        toastManager.warning(
          'Camera transport unavailable',
          'Live local preview is active, but the remote camera MediaTrack session could not be established.'
        );
      } finally {
        cameraSessionSourceIdsRef.current.delete(sourceId);
      }
    },
    [dispatch, sendCameraControlRequest, sourcesRef, toastManager]
  );

  const replaceCameraStream = useCallback(
    async (sourceId: string, stream: MediaStream): Promise<void> => {
      const source = sourcesRef.current.find((item) => item.id === sourceId);
      if (!source?.sessionId) {
        return;
      }
      const videoTrack = stream.getVideoTracks()[0] ?? null;
      if (!videoTrack) {
        logger.warn('replaceCameraStream: stream has no video track', {
          'source.id': sourceId,
        });
        return;
      }
      const ok = await webrtcService.replaceLocalVideoTrack(source.sessionId, videoTrack);
      if (!ok) {
        logger.error('replaceCameraStream: failed to replace track', {
          'source.id': sourceId,
          'session.id': source.sessionId,
        });
      }
    },
    [sourcesRef]
  );

  return { createCameraSession, replaceCameraStream, sendCameraControlRequest };
}
