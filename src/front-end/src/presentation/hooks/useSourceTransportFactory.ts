import { useCallback, useRef } from 'react';
import type { Dispatch, RefObject } from 'react';
import type { InputSource } from '@/gen/config_service_pb';
import type { IStatsDisplay, ICameraPreview, IToastDisplay } from '@/infrastructure/transport/transport-types';
import { WebRTCFrameTransportService } from '@/infrastructure/transport/webrtc-frame-transport';
import type { ActiveFilterState } from '@/presentation/components/filters/FilterPanel';
import type { GridSource, GridSourceAction } from '@/presentation/utils/grid-source';
import { GridSourceActionType } from '@/presentation/utils/grid-source';
import { filtersToFilterData } from '@/presentation/utils/grid-source';
import { hasModelInferenceFilter } from '@/presentation/utils/grid-source';
import { frameResponseToDataUrl } from '@/presentation/utils/image-utils';
import { statsFrameToMetrics } from '@/presentation/utils/metric-point';

type SourceTransportFactoryOptions = {
  nextNumberRef: RefObject<number>;
  statsManager: Pick<IStatsDisplay, 'updateCameraStatus' | 'updateTransportStatus' | 'updateProcessingStats'>;
  toastManager: IToastDisplay;
  dispatch: Dispatch<GridSourceAction>;
  activeFiltersRef: RefObject<ActiveFilterState[]>;
  selectedResolutionRef: RefObject<string>;
  selectedAcceleratorRef: RefObject<'gpu' | 'cpu'>;
  sourcesRef: RefObject<GridSource[]>;
};

type SourceTransportFactoryResult = {
  buildSource: (inputSource: InputSource) => GridSource;
  clearSourceMetrics: (sourceId: string) => void;
};

export function useSourceTransportFactory({
  nextNumberRef,
  statsManager,
  toastManager,
  dispatch,
  activeFiltersRef,
  selectedResolutionRef,
  selectedAcceleratorRef,
  sourcesRef,
}: SourceTransportFactoryOptions): SourceTransportFactoryResult {
  const cameraFrameTimeRef = useRef<Record<string, number>>({});
  const fpsTimestampsRef = useRef<Record<string, number[]>>({});
  const fpsLastUiUpdateRef = useRef<Record<string, number>>({});
  const fpsLastValueRef = useRef<Record<string, number>>({});

  const clearSourceMetrics = useCallback((sourceId: string): void => {
    delete cameraFrameTimeRef.current[sourceId];
    delete fpsTimestampsRef.current[sourceId];
    delete fpsLastUiUpdateRef.current[sourceId];
    delete fpsLastValueRef.current[sourceId];
  }, []);

  const buildSource = useCallback(
    (inputSource: InputSource): GridSource => {
      const number = nextNumberRef.current;
      nextNumberRef.current += 1;

      const uniqueId = `${inputSource.id}-${number}`;
      const sourceImagePath =
        inputSource.type === 'video'
          ? inputSource.previewImagePath || ''
          : inputSource.imagePath;

      const activeFilters = activeFiltersRef.current;
      const selectedResolution = selectedResolutionRef.current;
      const selectedAccelerator = selectedAcceleratorRef.current;

      const cameraManager: ICameraPreview = {
        setProcessing: () => undefined,
        getLastFrameTime: () => cameraFrameTimeRef.current[uniqueId] ?? performance.now(),
      };

      const transport =
        inputSource.type === 'camera'
          ? null
          : new WebRTCFrameTransportService(
              uniqueId,
              statsManager as IStatsDisplay,
              cameraManager,
              toastManager
            );

      if (transport) {
        transport.connect();
        transport.onFrameResult((data) => {
          if (!data.imageData?.byteLength || data.width <= 0 || data.height <= 0) return;
          const now = performance.now();
          cameraFrameTimeRef.current[uniqueId] = now;

          if (inputSource.type === 'video') {
            const existing = fpsTimestampsRef.current[uniqueId] ?? [];
            existing.push(now);

            const cutoff = now - 1000;
            while (existing.length > 0 && existing[0] < cutoff) {
              existing.shift();
            }
            fpsTimestampsRef.current[uniqueId] = existing;

            const windowDurationMs =
              existing.length > 1 ? Math.max(1, existing[existing.length - 1] - existing[0]) : 1000;
            const fps = existing.length > 1 ? ((existing.length - 1) * 1000) / windowDurationMs : 0;
            const lastUiUpdate = fpsLastUiUpdateRef.current[uniqueId] ?? 0;
            const lastValue = fpsLastValueRef.current[uniqueId] ?? 0;
            const shouldUpdateUi = now - lastUiUpdate >= 200 || Math.abs(fps - lastValue) >= 0.5;

            if (shouldUpdateUi) {
              fpsLastUiUpdateRef.current[uniqueId] = now;
              fpsLastValueRef.current[uniqueId] = fps;
              dispatch({
                type: GridSourceActionType.SET_SOURCE_FPS,
                payload: {
                  sourceId: uniqueId,
                  fps,
                },
              });
            }

            dispatch({
              type: GridSourceActionType.SET_SOURCE_RESOLUTION,
              payload: {
                sourceId: uniqueId,
                width: data.width,
                height: data.height,
              },
            });
          }

          dispatch({
            type: GridSourceActionType.SET_CURRENT_IMAGE,
            payload: {
              sourceId: uniqueId,
              currentImageSrc: frameResponseToDataUrl(
                data.imageData,
                data.width,
                data.height,
                data.channels || 4
              ),
            },
          });
        });
        transport.onDetectionResult((frame) => {
          const sourceSnapshot = sourcesRef.current.find((item) => item.id === uniqueId);
          const shouldShowDetections = sourceSnapshot
            ? hasModelInferenceFilter(sourceSnapshot.filters)
            : false;

          if (!shouldShowDetections) {
            dispatch({
              type: GridSourceActionType.SET_DETECTIONS,
              payload: {
                sourceId: uniqueId,
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
              sourceId: uniqueId,
              detections: frame.detections,
              width: frame.imageWidth,
              height: frame.imageHeight,
            },
          });
        });
        transport.onStatsResult((statsFrame) => {
          const metrics = statsFrameToMetrics(statsFrame);
          if (!Object.keys(metrics).length) {
            return;
          }
          dispatch({
            type: GridSourceActionType.SET_SOURCE_METRICS,
            payload: {
              sourceId: uniqueId,
              metrics,
            },
          });
        });
      }

      const filters =
        activeFilters.length > 0
          ? activeFilters.map((f) => ({ id: f.id, parameters: { ...f.parameters } }))
          : [{ id: 'none', parameters: {} }];

      if (transport) {
        if (inputSource.type === 'video') {
          waitForConnected(transport, () => {
            dispatch({ type: GridSourceActionType.SET_CONNECTED, payload: { sourceId: uniqueId, connected: true } });
            transport.sendStartVideo(inputSource.id, filtersToFilterData(filters), selectedAccelerator);
          });
        } else {
          waitForConnected(transport, () => {
            dispatch({ type: GridSourceActionType.SET_CONNECTED, payload: { sourceId: uniqueId, connected: true } });
          });
        }
      }

      return {
        id: uniqueId,
        number,
        name: inputSource.displayName,
        type: inputSource.type,
        imagePath: sourceImagePath,
        originalImageSrc: sourceImagePath,
        currentImageSrc: sourceImagePath,
        transport,
        remoteStream: null,
        sessionId: null,
        sessionMode: inputSource.type === 'camera' ? 'camera-mediatrack' : 'frame-processing',
        filters,
        resolution: selectedResolution || 'original',
        accelerator: selectedAccelerator || 'gpu',
        videoId: inputSource.type === 'video' ? inputSource.id : undefined,
        detections: [],
        detectionImageWidth: 0,
        detectionImageHeight: 0,
        fps: 0,
        displayWidth: 0,
        displayHeight: 0,
        connected: false,
        metrics: {},
      };
    },
    [activeFiltersRef, dispatch, nextNumberRef, selectedAcceleratorRef, selectedResolutionRef, sourcesRef, statsManager, toastManager]
  );

  return { buildSource, clearSourceMetrics };
}

function waitForConnected(
  transport: WebRTCFrameTransportService,
  onConnected: () => void
): void {
  const check = (): void => {
    if (transport.isConnected()) {
      onConnected();
    } else {
      setTimeout(check, 100);
    }
  };
  setTimeout(check, 100);
}
