import { type ReactElement } from 'react';
import { CameraPreview } from './CameraPreview';
import { VideoSourceCard } from './VideoSourceCard';
import { SOURCE_TYPES } from './SourceDetailsBadge';
import { useVideoGridContext } from '@/presentation/context/video-grid-context';
import styles from './VideoGrid.module.css';

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

function getGridClass(count: number): string {
  if (count <= 1) return styles.grid1;
  if (count === 2) return styles.grid2;
  if (count <= 4) return styles.grid4;
  if (count <= 6) return styles.grid6;
  return styles.grid9;
}

type VideoGridProps = {
  'data-testid'?: string;
};

export function VideoGrid({ 'data-testid': dataTestId }: VideoGridProps): ReactElement {
  const {
    sources,
    onCameraError,
    onCameraFrame,
    onCameraStatus,
    onCameraStreamReady,
    onSourceFpsUpdate,
    onSourceResolutionUpdate,
  } = useVideoGridContext();

  return (
    <div className={styles.shell}>
      <div
        data-testid={dataTestId ?? 'video-grid'}
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
                source={source}
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
