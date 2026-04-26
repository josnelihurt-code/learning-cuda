import { useMemo, useState, type ReactNode, type ReactElement } from 'react';
import { colorForClassId } from '@/presentation/utils/detection-colors';
import { useVideoGridContext } from '@/presentation/context/video-grid-context';
import type { GridSource } from '@/presentation/utils/grid-source';
import { SourceDetailsBadge, SOURCE_TYPES, normalizeSourceType, type SourceType } from './SourceDetailsBadge';
import styles from './VideoSourceCard.module.css';

type VideoSourceCardProps = {
  source: GridSource;
  children?: ReactNode;
};

export function VideoSourceCard({
  source,
  children,
}: VideoSourceCardProps): ReactElement {
  const { selectedSourceId, onChangeImageRequest, onCloseSource, onSelectSource } = useVideoGridContext();
  const sourceType: SourceType = normalizeSourceType(source.type);
  const [staticImageResolution, setStaticImageResolution] = useState<{ width: number; height: number } | null>(null);
  const isSelected = selectedSourceId === source.id;
  const cardClassName = isSelected ? `${styles.card} ${styles.selected}` : styles.card;
  const imageSrc = source.currentImageSrc || source.imagePath;
  const shouldRenderProcessedImage = Boolean(imageSrc);
  const hasDetections =
    Array.isArray(source.detections) &&
    source.detections.length > 0 &&
    (source.detectionImageWidth ?? 0) > 0 &&
    (source.detectionImageHeight ?? 0) > 0;
  const sourceDetailsWidth = useMemo(() => {
    if ((source.displayWidth ?? 0) > 0) {
      return source.displayWidth;
    }
    if (sourceType === SOURCE_TYPES.STATIC) {
      return staticImageResolution?.width;
    }
    return undefined;
  }, [source.displayWidth, sourceType, staticImageResolution?.width]);
  const sourceDetailsHeight = useMemo(() => {
    if ((source.displayHeight ?? 0) > 0) {
      return source.displayHeight;
    }
    if (sourceType === SOURCE_TYPES.STATIC) {
      return staticImageResolution?.height;
    }
    return undefined;
  }, [source.displayHeight, sourceType, staticImageResolution?.height]);

  return (
    <div className={styles.source}>
      <div
        className={cardClassName}
        onClick={() => onSelectSource(source.id)}
        data-testid={`source-card-${source.number}`}
        data-source-id={source.id}
      >
        <div className={styles.sourceNumber}>{source.number}</div>
        <SourceDetailsBadge
          sourceType={sourceType}
          fps={source.fps}
            width={sourceDetailsWidth}
            height={sourceDetailsHeight}
            metrics={source.metrics}
          className={styles.sourceDetails}
          stopPropagationOnToggle={true}
          testId={`source-fps-${source.number}`}
        />
        {sourceType === 'static' ? (
          <button
            className={styles.changeImageBtn}
            onClick={(event) => {
              event.stopPropagation();
              onChangeImageRequest(source.id);
            }}
            title="Change image"
            data-testid="change-image-button"
          >
            {'\u27f3'}
          </button>
        ) : null}
        <button
          className={styles.closeBtn}
          onClick={(event) => {
            event.stopPropagation();
            onCloseSource(source.id);
          }}
          title={`Close ${source.name}`}
          data-testid="source-close-button"
        >
          {'\u00d7'}
        </button>
        <div className={styles.content}>
          {shouldRenderProcessedImage ? (
            <img
              src={imageSrc}
              alt={source.name}
              crossOrigin="anonymous"
              onLoad={(event) => {
                if (sourceType !== SOURCE_TYPES.STATIC) {
                  return;
                }
                const nextWidth = event.currentTarget.naturalWidth;
                const nextHeight = event.currentTarget.naturalHeight;
                if (nextWidth > 0 && nextHeight > 0) {
                  setStaticImageResolution({ width: nextWidth, height: nextHeight });
                }
              }}
            />
          ) : null}
          {children}
          {hasDetections ? (
            <svg
              className={styles.detectionOverlay}
              viewBox={`0 0 ${source.detectionImageWidth} ${source.detectionImageHeight}`}
              preserveAspectRatio="xMidYMid meet"
              data-testid="detection-overlay"
            >
              {source.detections.map((detection, index) => {
                const color = colorForClassId(detection.classId);
                const label = `${detection.className || `class ${detection.classId}`} ${Math.round(detection.confidence * 100)}%`;
                return (
                  <g key={`${detection.classId}-${index}`}>
                    <rect
                      x={detection.x}
                      y={detection.y}
                      width={detection.width}
                      height={detection.height}
                      fill="none"
                      stroke={color}
                      strokeWidth={Math.max(2, source.detectionImageWidth / 320)}
                      vectorEffect="non-scaling-stroke"
                    />
                    <text
                      x={detection.x + 4}
                      y={Math.max(detection.y - 4, 12)}
                      fill={color}
                      fontSize={Math.max(12, source.detectionImageWidth / 40)}
                      fontFamily="sans-serif"
                      stroke="rgba(0,0,0,0.7)"
                      strokeWidth={0.5}
                      paintOrder="stroke"
                    >
                      {label}
                    </text>
                  </g>
                );
              })}
            </svg>
          ) : null}
        </div>
      </div>
    </div>
  );
}
