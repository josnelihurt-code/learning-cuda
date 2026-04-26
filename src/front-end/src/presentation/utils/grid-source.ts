import type { WebRTCFrameTransportService } from '@/infrastructure/transport/webrtc-frame-transport';
import type { ActiveFilterState } from '@/presentation/components/filters/FilterPanel';
import type { Detection } from '@/gen/image_processor_service_pb';
import { FilterData } from '@/domain/value-objects';

export type GridSourceSessionMode = 'frame-processing' | 'camera-mediatrack';

export type GridSource = {
  id: string;
  number: number;
  name: string;
  type: string;
  imagePath: string;
  originalImageSrc: string;
  currentImageSrc: string;
  transport: WebRTCFrameTransportService | null;
  remoteStream: MediaStream | null;
  sessionId: string | null;
  sessionMode: GridSourceSessionMode;
  filters: ActiveFilterState[];
  resolution: string;
  accelerator: 'gpu' | 'cpu';
  videoId?: string;
  detections: Detection[];
  detectionImageWidth: number;
  detectionImageHeight: number;
  fps: number;
  displayWidth: number;
  displayHeight: number;
  connected: boolean;
  metrics: Record<string, string>;
};

export enum GridSourceActionType {
  ADD_SOURCE = 'ADD_SOURCE',
  REMOVE_SOURCE = 'REMOVE_SOURCE',
  UPDATE_SOURCE = 'UPDATE_SOURCE',
  SET_CONNECTED = 'SET_CONNECTED',
  SET_REMOTE_STREAM = 'SET_REMOTE_STREAM',
  SET_SESSION = 'SET_SESSION',
  SET_CURRENT_IMAGE = 'SET_CURRENT_IMAGE',
  SET_DETECTIONS = 'SET_DETECTIONS',
  SET_SOURCE_FPS = 'SET_SOURCE_FPS',
  SET_SOURCE_RESOLUTION = 'SET_SOURCE_RESOLUTION',
  SET_SOURCE_METRICS = 'SET_SOURCE_METRICS',
  SYNC_FILTERS = 'SYNC_FILTERS',
}

export type GridSourceAction =
  | { type: GridSourceActionType.ADD_SOURCE; payload: GridSource }
  | { type: GridSourceActionType.REMOVE_SOURCE; payload: { sourceId: string } }
  | { type: GridSourceActionType.UPDATE_SOURCE; payload: { sourceId: string; updater: (s: GridSource) => GridSource } }
  | { type: GridSourceActionType.SET_CONNECTED; payload: { sourceId: string; connected: boolean } }
  | { type: GridSourceActionType.SET_REMOTE_STREAM; payload: { sourceId: string; remoteStream: MediaStream } }
  | { type: GridSourceActionType.SET_SESSION; payload: { sourceId: string; sessionId: string; sessionMode: GridSourceSessionMode } }
  | { type: GridSourceActionType.SET_CURRENT_IMAGE; payload: { sourceId: string; currentImageSrc: string } }
  | { type: GridSourceActionType.SET_DETECTIONS; payload: { sourceId: string; detections: Detection[]; width: number; height: number } }
  | { type: GridSourceActionType.SET_SOURCE_FPS; payload: { sourceId: string; fps: number } }
  | { type: GridSourceActionType.SET_SOURCE_RESOLUTION; payload: { sourceId: string; width: number; height: number } }
  | { type: GridSourceActionType.SET_SOURCE_METRICS; payload: { sourceId: string; metrics: Record<string, string> } }
  | { type: GridSourceActionType.SYNC_FILTERS; payload: { sourceId: string; filters: ActiveFilterState[]; resolution: string; accelerator: 'gpu' | 'cpu' } };

export function filtersToFilterData(filters: ActiveFilterState[]): FilterData[] {
  return filters.map((f) => new FilterData(f.id, { ...f.parameters }));
}

export function normalizeFilters(filters: ActiveFilterState[]): ActiveFilterState[] {
  return filters.length > 0
    ? filters.map((f) => ({ id: f.id, parameters: { ...f.parameters } }))
    : [{ id: 'none', parameters: {} }];
}

export function hasModelInferenceFilter(filters: ActiveFilterState[]): boolean {
  return filters.some((filter) => filter.id === 'model_inference');
}
