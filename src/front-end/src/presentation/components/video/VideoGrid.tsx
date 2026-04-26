import { type ReactElement } from 'react';
import { CameraPreview } from './CameraPreview';
import { VideoSourceCard } from './VideoSourceCard';
import { SOURCE_TYPES, type SourceType } from './SourceDetailsBadge';
import type { GridSourceView } from '@/presentation/utils/grid-source';
import styles from './VideoGrid.module.css';

type CameraFramePayload = {
  base64data: string;
  width: number;
  height: number;
  timestamp: number;
};

type CameraDimensions = {
  width?: number;
  height?: number;
};

function getCameraDimensionsForResolution(
  resolution: string | undefined,
  currentWidth: number | undefined,
  currentHeight: number | undefined
): CameraDimensions {
  if (resolution === 'original') {
    return {};
  }

  const baseWidth = (currentWidth ?? 0) > 0 ? currentWidth! : 1280;
  const baseHeight = (currentHeight ?? 0) > 0 ? currentHeight! : 720;
  const scale = resolution === 'quarter' ? 0.25 : 0.5;

  return {
    width: Math.max(160, Math.round(baseWidth * scale)),
    height: Math.max(120, Math.round(baseHeight * scale)),
  };
}

type VideoGridProps = {
  sources: GridSourceView[];
  selectedSourceId: string | null;
  onSelectSource: (sourceId: string) => void;
  onCloseSource: (sourceId: string) => void;
  onChangeImageRequest: (sourceId: string, sourceNumber: number) => void;
  onCameraFrame: (sourceId: string, payload: CameraFramePayload) => void;
  onCameraStreamReady: (sourceId: string, stream: MediaStream) => void;
  onCameraStatus: (status: string, type: 'success' | 'error' | 'warning' | 'inactive') => void;
  onCameraError: (title: string, message: string) => void;
  onSourceFpsUpdate: (sourceId: string, fps: number) => void;
  onSourceResolutionUpdate: (sourceId: string, width: number, height: number) => void;
};

function getGridClass(count: number): string {
  if (count <= 1) return styles.grid1;
  if (count === 2) return styles.grid2;
  if (count <= 4) return styles.grid4;
  if (count <= 6) return styles.grid6;
  return styles.grid9;
}

export function VideoGrid({
  sources,
  selectedSourceId,
  onSelectSource,
  onCloseSource,
  onChangeImageRequest,
  onCameraFrame,
  onCameraStreamReady,
  onCameraStatus,
  onCameraError,
  onSourceFpsUpdate,
  onSourceResolutionUpdate,
}: VideoGridProps): ReactElement {
  const asSourceType = (value: string): SourceType => {
    if (
      value === SOURCE_TYPES.CAMERA ||
      value === SOURCE_TYPES.VIDEO ||
      value === SOURCE_TYPES.STATIC
    ) {
      return value;
    }
    return SOURCE_TYPES.OTHER;
  };

  return (
    <div className={styles.shell}>
      <div
        data-testid="video-grid"
        className={`${styles.grid} ${getGridClass(sources.length)}`}
      >
        {sources.map((source) => (
          (() => {
            const cameraDimensions = getCameraDimensionsForResolution(
              source.resolution,
              source.displayWidth,
              source.displayHeight
            );
            return (
              <VideoSourceCard
                key={source.id}
                sourceId={source.id}
                sourceNumber={source.number}
                sourceName={source.name}
                sourceType={asSourceType(source.type)}
                imageSrc={source.imageSrc}
                isSelected={selectedSourceId === source.id}
                onSelect={onSelectSource}
                onClose={onCloseSource}
                onChangeImage={onChangeImageRequest}
                detections={source.detections}
                detectionImageWidth={source.detectionImageWidth}
                detectionImageHeight={source.detectionImageHeight}
                fps={source.fps}
                displayWidth={source.displayWidth}
                displayHeight={source.displayHeight}
              >
                {source.type === SOURCE_TYPES.CAMERA ? (
                  <CameraPreview
                    width={cameraDimensions.width}
                    height={cameraDimensions.height}
                    captureFrames={false}
                    remoteStream={source.remoteStream ?? null}
                    activeFilters={source.filters ?? []}
                    onFrameCaptured={(payload) => onCameraFrame(source.id, payload)}
                    onStreamReady={(stream) => onCameraStreamReady(source.id, stream)}
                    onCameraStatus={onCameraStatus}
                    onCameraError={onCameraError}
                    onFpsUpdate={(fps) => onSourceFpsUpdate(source.id, fps)}
                    onResolutionUpdate={(width, height) =>
                      onSourceResolutionUpdate(source.id, width, height)
                    }
                  />
                ) : null}
              </VideoSourceCard>
            );
          })()
        ))}
      </div>
    </div>
  );
}
