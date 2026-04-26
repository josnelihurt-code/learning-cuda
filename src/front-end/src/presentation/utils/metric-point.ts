import type { MetricPoint, ProcessingStatsFrame } from '@/gen/image_processor_service_pb';

export function metricPointToDisplayValue(metric: MetricPoint): string {
  const raw = metric.value?.value;
  let value = '';

  if (raw?.case === 'doubleValue') {
    value = raw.value.toFixed(2);
  } else if (raw?.case === 'int64Value') {
    value = raw.value.toString();
  } else if (raw?.case === 'stringValue') {
    value = raw.value;
  }

  return metric.unit ? `${value} ${metric.unit}`.trim() : value;
}

export function statsFrameToMetrics(frame: ProcessingStatsFrame): Record<string, string> {
  const metrics: Record<string, string> = {};
  for (const metric of frame.metrics) {
    const value = metricPointToDisplayValue(metric);
    if (!value) continue;
    metrics[metric.key] = value;
  }
  return metrics;
}
