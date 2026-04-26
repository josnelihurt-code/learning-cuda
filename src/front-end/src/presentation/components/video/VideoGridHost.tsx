import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { InputSource } from '@/gen/config_service_pb';
import type { StaticImage } from '@/gen/common_pb';
import type { IToastDisplay } from '@/infrastructure/transport/transport-types';
import { webrtcService } from '@/infrastructure/connection/webrtc-service';
import { logger } from '@/infrastructure/observability/otel-logger';
import { useAppServices } from '@/presentation/providers/app-services-provider';
import { useDashboardState } from '@/presentation/context/dashboard-state-context';
import { useToast } from '@/presentation/hooks/useToast';
import { useProcessingStats } from '@/presentation/hooks/useProcessingStats';
import { useFilterApplication } from '@/presentation/hooks/useFilterApplication';
import { useCameraTransport } from '@/presentation/hooks/useCameraTransport';
import { useSourceTransportFactory } from '@/presentation/hooks/useSourceTransportFactory';
import { useSourceFilterSync } from '@/presentation/hooks/useSourceFilterSync';
import { useGridSources } from '@/presentation/hooks/useGridSources';
import { useLatest } from '@/presentation/hooks/useLatest';
import { VideoGrid } from '@/presentation/components/video/VideoGrid';
import { SourceDrawer } from '@/presentation/components/video/SourceDrawer';
import { AddSourceFab } from '@/presentation/components/video/AddSourceFab';
import { ImageSelectorModal } from '@/presentation/components/video/ImageSelectorModal';
import { AcceleratorStatusFab } from '@/presentation/components/video/AcceleratorStatusFab';
import { StatsPanel as ReactStatsPanel } from '@/presentation/components/app/StatsPanel';
import { filtersToFilterData, GridSourceActionType, type GridSource } from '@/presentation/utils/grid-source';

const MAX_SOURCES = 9;

export function VideoGridHost(): React.ReactNode {
  const { container, ready } = useAppServices();
  const {
    activeFilters,
    selectedAccelerator,
    selectedResolution,
    setSelectedSource,
    setActiveFilters,
    setResolution,
    setWebRTCReady,
  } = useDashboardState();
  const toast = useToast();

  const toastManager = useMemo<IToastDisplay>(
    () => ({
      success: toast.success,
      error: toast.error,
      warning: toast.warning,
      info: toast.info,
    }),
    [toast.error, toast.info, toast.success, toast.warning]
  );

  const { statsManager } = useProcessingStats();
  const { sources, selectedSourceId, sourcesRef, selectedSourceIdRef, nextNumberRef, dispatch, setSelectedSource: setSelectedSourceId, removeSource } =
    useGridSources();
  const { createCameraSession, replaceCameraStream, sendCameraControlRequest } = useCameraTransport({
    dispatch,
    sourcesRef,
    toastManager,
  });

  const onCameraStreamReady = useCallback(
    (sourceId: string, stream: MediaStream): void => {
      const source = sourcesRef.current.find((item) => item.id === sourceId);
      if (source?.sessionId) {
        void replaceCameraStream(sourceId, stream);
      } else {
        void createCameraSession(sourceId, stream);
      }
    },
    [createCameraSession, replaceCameraStream, sourcesRef]
  );

  const activeFiltersRef = useLatest(activeFilters);
  const selectedResolutionRef = useLatest(selectedResolution);
  const selectedAcceleratorRef = useLatest(selectedAccelerator);

  const { buildSource, clearSourceMetrics } = useSourceTransportFactory({
    nextNumberRef,
    statsManager,
    toastManager,
    dispatch,
    activeFiltersRef,
    selectedResolutionRef,
    selectedAcceleratorRef,
    sourcesRef,
  });
  const { applyStaticFilters, applyVideoFilters } = useFilterApplication(toastManager);

  const [isDrawerOpen, setIsDrawerOpen] = useState(false);
  const [isImageSelectorOpen, setIsImageSelectorOpen] = useState(false);
  const [availableSources, setAvailableSources] = useState<InputSource[]>([]);
  const [availableImages, setAvailableImages] = useState<StaticImage[]>([]);
  const pendingImageChangeSourceIdRef = useRef<string | null>(null);
  const defaultSourceInitializedRef = useRef(false);

  const syncSelectionToDashboard = useCallback(
    (source: GridSource | null): void => {
      if (!source) return;
      setSelectedSource(source.number, source.name);
      setActiveFilters(source.filters.map((f) => ({ id: f.id, parameters: { ...f.parameters } })));
      setResolution(source.resolution);
    },
    [setActiveFilters, setResolution, setSelectedSource]
  );

  useEffect(() => {
    const selectedSource = sources.find((s) => s.id === selectedSourceId);
    setWebRTCReady(selectedSource?.connected ?? false);
  }, [sources, selectedSourceId, setWebRTCReady]);

  useSourceFilterSync({
    ready,
    selectedSourceId,
    sourcesRef,
    activeFilters,
    selectedAccelerator,
    selectedResolution,
    dispatch,
    sendCameraControlRequest,
    applyStaticFilters,
    applyVideoFilters,
    toastManager,
  });

  useEffect(() => {
    if (!ready || defaultSourceInitializedRef.current) return;
    const defaultSource = container.getInputSourceService().getDefaultSource();
    if (defaultSource && sourcesRef.current.length === 0) {
      defaultSourceInitializedRef.current = true;
      const source = buildSource(defaultSource);
      dispatch({ type: GridSourceActionType.ADD_SOURCE, payload: source });
      setSelectedSourceId(source.id);
      syncSelectionToDashboard(source);
    }
  }, [buildSource, container, dispatch, ready, setSelectedSourceId, sourcesRef, syncSelectionToDashboard]);

  const addSource = useCallback(
    (inputSource: InputSource): void => {
      if (sourcesRef.current.length >= MAX_SOURCES) {
        toast?.warning('Maximum Sources', `Cannot add more than ${MAX_SOURCES} sources`);
        return;
      }
      const isFirst = sourcesRef.current.length === 0;
      const source = buildSource(inputSource);
      dispatch({ type: GridSourceActionType.ADD_SOURCE, payload: source });
      if (isFirst) {
        setSelectedSourceId(source.id);
        syncSelectionToDashboard(source);
      }
    },
    [buildSource, dispatch, setSelectedSourceId, sourcesRef, syncSelectionToDashboard, toast]
  );

  const openDrawer = useCallback((): void => {
    setAvailableSources(container.getInputSourceService().getSources());
    setIsDrawerOpen(true);
  }, [container]);

  const refreshDrawerSources = useCallback((): void => {
    setAvailableSources(container.getInputSourceService().getSources());
  }, [container]);

  const onSelectSourceFromDrawer = useCallback(
    (source: InputSource): void => {
      addSource(source);
      setIsDrawerOpen(false);
    },
    [addSource]
  );

  const onSelectImage = useCallback(
    (image: StaticImage): void => {
      const sourceId = pendingImageChangeSourceIdRef.current;
      if (sourceId !== null) {
        dispatch({
          type: GridSourceActionType.UPDATE_SOURCE,
          payload: {
            sourceId,
            updater: (s) => ({
              ...s,
              imagePath: image.path,
              originalImageSrc: image.path,
              currentImageSrc: image.path,
            }),
          },
        });
      }
      pendingImageChangeSourceIdRef.current = null;
      setIsImageSelectorOpen(false);
    },
    [dispatch]
  );

  const activeTransportService = useMemo(() => {
    const selected = selectedSourceId
      ? sources.find((s) => s.id === selectedSourceId)
      : null;
    return selected?.transport ?? sources.find((s) => s.transport)?.transport ?? null;
  }, [selectedSourceId, sources]);

  const selectedSourceDetails = useMemo(() => {
    const selected = selectedSourceId ? sources.find((source) => source.id === selectedSourceId) : null;
    if (!selected) {
      return null;
    }
    return {
      type: selected.type,
      fps: selected.fps,
      width: selected.displayWidth,
      height: selected.displayHeight,
    };
  }, [selectedSourceId, sources]);

  useEffect(() => {
    const stopAllSources = (): void => {
      sourcesRef.current.forEach((source) => {
        source.transport?.disconnect();
        if (source.sessionId) {
          void webrtcService.closeSession(source.sessionId);
        }
      });
    };
    window.addEventListener('beforeunload', stopAllSources);
    return () => {
      window.removeEventListener('beforeunload', stopAllSources);
    };
  }, [sourcesRef]);

  return (
    <>
      <VideoGrid
        sources={sources.map((source) => ({
          id: source.id,
          number: source.number,
          name: source.name,
          type: source.type,
          resolution: source.resolution,
          imageSrc: source.currentImageSrc || source.imagePath,
          remoteStream: source.remoteStream,
          filters: source.filters,
          detections: source.detections,
          detectionImageWidth: source.detectionImageWidth,
          detectionImageHeight: source.detectionImageHeight,
          fps: source.fps,
          displayWidth: source.displayWidth,
          displayHeight: source.displayHeight,
        }))}
        selectedSourceId={selectedSourceId}
        onSelectSource={(sourceId) => {
          setSelectedSourceId(sourceId);
          const source = sourcesRef.current.find((s) => s.id === sourceId) ?? null;
          syncSelectionToDashboard(source);
        }}
        onCloseSource={(sourceId) => {
          const source = sourcesRef.current.find((s) => s.id === sourceId);
          const isSelected = sourceId === selectedSourceIdRef.current;
          source?.transport?.disconnect();
          clearSourceMetrics(sourceId);
          if (source?.sessionId) {
            void webrtcService.closeSession(source.sessionId).catch((error) => {
              logger.error('Failed to close camera MediaTrack session', {
                'error.message': error instanceof Error ? error.message : String(error),
                'source.id': sourceId,
              });
            });
          }
          removeSource(sourceId);
          if (isSelected) {
            const remaining = sourcesRef.current.filter((s) => s.id !== sourceId);
            syncSelectionToDashboard(remaining[0] ?? null);
          }
        }}
        onChangeImageRequest={(sourceId) => {
          pendingImageChangeSourceIdRef.current = sourceId;
          void container
            .getInputSourceService()
            .listAvailableImages()
            .then((images) => {
              setAvailableImages(images);
              setIsImageSelectorOpen(true);
            })
            .catch((error) => {
              logger.error('Failed to load image options', {
                'error.message': error instanceof Error ? error.message : String(error),
              });
            });
        }}
        onCameraFrame={(sourceId, payload) => {
          const cameraSource = sourcesRef.current.find((item) => item.id === sourceId);
          if (!cameraSource || cameraSource.sessionMode === 'camera-mediatrack') return;
          if (!cameraSource.transport?.isConnected()) return;
          const filters = cameraSource.filters.length
            ? cameraSource.filters
            : [{ id: 'none', parameters: {} }];
          cameraSource.transport.sendFrame(
            `data:image/jpeg;base64,${payload.base64data}`,
            payload.width,
            payload.height,
            filtersToFilterData(filters),
            selectedAcceleratorRef.current
          );
        }}
        onCameraStreamReady={onCameraStreamReady}
        onCameraStatus={(status, type) => statsManager.updateCameraStatus(status, type)}
        onCameraError={(title, message) => toastManager.error(title, message)}
        onSourceFpsUpdate={(sourceId, fps) => {
          dispatch({
            type: GridSourceActionType.SET_SOURCE_FPS,
            payload: {
              sourceId,
              fps,
            },
          });
        }}
        onSourceResolutionUpdate={(sourceId, width, height) => {
          dispatch({
            type: GridSourceActionType.SET_SOURCE_RESOLUTION,
            payload: {
              sourceId,
              width,
              height,
            },
          });
        }}
        data-testid="video-grid-host"
      />
      <AddSourceFab onClick={openDrawer} />
      <AcceleratorStatusFab />
      <SourceDrawer
        isOpen={isDrawerOpen}
        availableSources={availableSources}
        onClose={() => setIsDrawerOpen(false)}
        onSelectSource={onSelectSourceFromDrawer}
        onSourcesChanged={refreshDrawerSources}
      />
      <ImageSelectorModal
        isOpen={isImageSelectorOpen}
        availableImages={availableImages}
        onClose={() => setIsImageSelectorOpen(false)}
        onSelectImage={onSelectImage}
      />
      <ReactStatsPanel
        selectedSource={selectedSourceDetails}
        transportService={activeTransportService}
      />
    </>
  );
}
