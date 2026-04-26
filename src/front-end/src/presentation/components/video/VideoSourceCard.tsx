import { useMemo, useState, type ReactNode, type ReactElement } from 'react';
import type { Detection } from '@/gen/image_processor_service_pb';
import { colorForClassId } from '@/presentation/utils/detection-colors';
import { SourceDetailsBadge, SOURCE_TYPES, type SourceType } from './SourceDetailsBadge';
import styles from './VideoSourceCard.module.css';

type VideoSourceCardProps = {
  sourceId: string;
  sourceNumber: number;
  sourceName: string;
  sourceType: SourceType;
  imageSrc: string;
  isSelected: boolean;
  onSelect: (sourceId: string) => void;
  onClose: (sourceId: string) => void;
  onChangeImage: (sourceId: string, sourceNumber: number) => void;
  children?: ReactNode;
  detections?: Detection[];
  detectionImageWidth?: number;
  detectionImageHeight?: number;
  fps?: number;
  displayWidth?: number;
  displayHeight?: number;
};

export function VideoSourceCard({
  sourceId,
  sourceNumber,
  sourceName,
  sourceType,
  imageSrc,
  isSelected,
  onSelect,
  onClose,
  onChangeImage,
  children,
  detections,
  detectionImageWidth,
  detectionImageHeight,
  fps = 0,
  displayWidth,
  displayHeight,
}: VideoSourceCardProps): ReactElement {
  const [staticImageResolution, setStaticImageResolution] = useState<{ width: number; height: number } | null>(null);
  const cardClassName = isSelected ? `${styles.card} ${styles.selected}` : styles.card;
  const shouldRenderProcessedImage = Boolean(imageSrc);
  const hasDetections =
    Array.isArray(detections) &&
    detections.length > 0 &&
    (detectionImageWidth ?? 0) > 0 &&
    (detectionImageHeight ?? 0) > 0;
  const sourceDetailsWidth = useMemo(() => {
    if ((displayWidth ?? 0) > 0) {
      return displayWidth;
    }
    if (sourceType === SOURCE_TYPES.STATIC) {
      return staticImageResolution?.width;
    }
    return undefined;
  }, [displayWidth, sourceType, staticImageResolution?.width]);
  const sourceDetailsHeight = useMemo(() => {
    if ((displayHeight ?? 0) > 0) {
      return displayHeight;
    }
    if (sourceType === SOURCE_TYPES.STATIC) {
      return staticImageResolution?.height;
    }
    return undefined;
  }, [displayHeight, sourceType, staticImageResolution?.height]);

  return (
    <div className={styles.source}>
      <div
        className={cardClassName}
        onClick={() => onSelect(sourceId)}
        data-testid={`source-card-${sourceNumber}`}
        data-source-id={sourceId}
      >
        <div className={styles.sourceNumber}>{sourceNumber}</div>
        <SourceDetailsBadge
          sourceType={sourceType}
          fps={fps}
            width={sourceDetailsWidth}
            height={sourceDetailsHeight}
          className={styles.sourceDetails}
          stopPropagationOnToggle={true}
          testId={`source-fps-${sourceNumber}`}
        />
        {sourceType === 'static' ? (
          <button
            className={styles.changeImageBtn}
            onClick={(event) => {
              event.stopPropagation();
              onChangeImage(sourceId, sourceNumber);
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
            onClose(sourceId);
          }}
          title={`Close ${sourceName}`}
          data-testid="source-close-button"
        >
          {'\u00d7'}
        </button>
        <div className={styles.content}>
          {shouldRenderProcessedImage ? (
            <img
              src={imageSrc}
              alt={sourceName}
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
              viewBox={`0 0 ${detectionImageWidth} ${detectionImageHeight}`}
              preserveAspectRatio="xMidYMid meet"
              data-testid="detection-overlay"
            >
              {detections!.map((detection, index) => {
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
                      strokeWidth={Math.max(2, detectionImageWidth! / 320)}
                      vectorEffect="non-scaling-stroke"
                    />
                    <text
                      x={detection.x + 4}
                      y={Math.max(detection.y - 4, 12)}
                      fill={color}
                      fontSize={Math.max(12, detectionImageWidth! / 40)}
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
