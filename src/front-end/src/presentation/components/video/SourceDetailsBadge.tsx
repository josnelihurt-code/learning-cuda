import { useMemo, useState, type MouseEvent, type ReactElement } from 'react';
import styles from './SourceDetailsBadge.module.css';

export const SOURCE_TYPES = {
  CAMERA: 'camera',
  VIDEO: 'video',
  STATIC: 'static',
  OTHER: 'other',
} as const;

export type SourceType = (typeof SOURCE_TYPES)[keyof typeof SOURCE_TYPES];

type SourceDetailsBadgeProps = {
  sourceType: SourceType;
  fps?: number;
  width?: number;
  height?: number;
  forceExpanded?: boolean;
  stopPropagationOnToggle?: boolean;
  className?: string;
  testId?: string;
  defaultExpanded?: boolean;
  layoutMode?: 'stack' | 'grid';
};

function normalizeSourceType(sourceType: SourceType): SourceType {
  if (
    sourceType === SOURCE_TYPES.CAMERA ||
    sourceType === SOURCE_TYPES.VIDEO ||
    sourceType === SOURCE_TYPES.STATIC
  ) {
    return sourceType;
  }
  return SOURCE_TYPES.OTHER;
}

function formatDimensions(width?: number, height?: number): string {
  if ((width ?? 0) > 0 && (height ?? 0) > 0) {
    return `${width}x${height}`;
  }
  return '--';
}

function sourceTypeLabel(sourceType: SourceType): string {
  const normalizedType = normalizeSourceType(sourceType);
  if (normalizedType === SOURCE_TYPES.CAMERA) return 'Webcam';
  if (normalizedType === SOURCE_TYPES.VIDEO) return 'Video';
  if (normalizedType === SOURCE_TYPES.STATIC) return 'Static';
  return 'Other';
}

export function SourceDetailsBadge({
  sourceType,
  fps = 0,
  width,
  height,
  forceExpanded = false,
  stopPropagationOnToggle = false,
  className,
  testId,
  defaultExpanded = true,
  layoutMode = 'stack',
}: SourceDetailsBadgeProps): ReactElement {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const isExpanded = forceExpanded || expanded;
  const normalizedType = normalizeSourceType(sourceType);
  const rootClassName = className ? `${styles.root} ${className}` : styles.root;
  const dimensions = useMemo(() => formatDimensions(width, height), [width, height]);
  const details = useMemo(() => {
    const rows: Array<{ label: string; value: string }> = [
      { label: 'Type', value: sourceTypeLabel(sourceType) },
      { label: 'Res', value: dimensions },
    ];

    if (normalizedType === SOURCE_TYPES.VIDEO || normalizedType === SOURCE_TYPES.CAMERA) {
      rows.unshift({ label: 'fps', value: fps.toFixed(1) });
    }

    return rows;
  }, [dimensions, fps, normalizedType, sourceType]);

  const toggleExpanded = (event: MouseEvent<HTMLButtonElement>): void => {
    if (stopPropagationOnToggle) {
      event.stopPropagation();
    }
    if (forceExpanded) {
      return;
    }
    setExpanded((current) => !current);
  };

  if (!isExpanded) {
    return (
      <button
        type="button"
        className={`${rootClassName} ${styles.collapsed}`}
        data-testid={testId}
        onClick={toggleExpanded}
        aria-label="Show source details"
        aria-expanded={false}
      />
    );
  }

  return (
    <button
      type="button"
      className={`${rootClassName} ${styles.expanded}`}
      data-testid={testId}
      onClick={toggleExpanded}
      aria-label={forceExpanded ? 'Source details' : 'Hide source details'}
      aria-expanded={true}
    >
      <div className={layoutMode === 'grid' ? styles.detailsGrid : styles.detailsStack}>
        {details.map((detail) => (
          <div key={detail.label} className={styles.detailRow}>
            <span className={styles.detailLabel}>{detail.label}:</span>
            <span className={styles.detailValue}>{detail.value}</span>
          </div>
        ))}
      </div>
      {/* TODO: Add more source-specific detail rows as telemetry grows. */}
    </button>
  );
}
