import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import type { Dispatch, RefObject } from 'react';
import type { GridSource, GridSourceAction } from '@/presentation/utils/grid-source';
import { GridSourceActionType } from '@/presentation/utils/grid-source';

function gridSourceReducer(state: GridSource[], action: GridSourceAction): GridSource[] {
  switch (action.type) {
    case GridSourceActionType.ADD_SOURCE:
      return [...state, action.payload];
    case GridSourceActionType.REMOVE_SOURCE:
      return state.filter((s) => s.id !== action.payload.sourceId);
    case GridSourceActionType.UPDATE_SOURCE:
      return state.map((s) => (s.id === action.payload.sourceId ? action.payload.updater(s) : s));
    case GridSourceActionType.SET_CONNECTED:
      return state.map((s) =>
        s.id === action.payload.sourceId ? { ...s, connected: action.payload.connected } : s
      );
    case GridSourceActionType.SET_REMOTE_STREAM:
      return state.map((s) =>
        s.id === action.payload.sourceId ? { ...s, remoteStream: action.payload.remoteStream } : s
      );
    case GridSourceActionType.SET_SESSION:
      return state.map((s) =>
        s.id === action.payload.sourceId
          ? { ...s, sessionId: action.payload.sessionId, sessionMode: action.payload.sessionMode, connected: true }
          : s
      );
    case GridSourceActionType.SET_CURRENT_IMAGE:
      return state.map((s) =>
        s.id === action.payload.sourceId ? { ...s, currentImageSrc: action.payload.currentImageSrc } : s
      );
    case GridSourceActionType.SET_DETECTIONS:
      return state.map((s) =>
        s.id === action.payload.sourceId
          ? {
              ...s,
              detections: action.payload.detections,
              detectionImageWidth: action.payload.width,
              detectionImageHeight: action.payload.height,
            }
          : s
      );
    case GridSourceActionType.SET_SOURCE_FPS:
      return state.map((s) =>
        s.id === action.payload.sourceId ? { ...s, fps: action.payload.fps } : s
      );
    case GridSourceActionType.SET_SOURCE_RESOLUTION:
      return state.map((s) =>
        s.id === action.payload.sourceId
          ? { ...s, displayWidth: action.payload.width, displayHeight: action.payload.height }
          : s
      );
    case GridSourceActionType.SET_SOURCE_METRICS:
      return state.map((s) =>
        s.id === action.payload.sourceId ? { ...s, metrics: action.payload.metrics } : s
      );
    case GridSourceActionType.SYNC_FILTERS:
      return state.map((s) =>
        s.id === action.payload.sourceId
          ? {
              ...s,
              filters: action.payload.filters,
              resolution: action.payload.resolution,
              accelerator: action.payload.accelerator,
            }
          : s
      );
    default:
      return state;
  }
}

type GridSourcesResult = {
  sources: GridSource[];
  selectedSourceId: string | null;
  sourcesRef: RefObject<GridSource[]>;
  selectedSourceIdRef: RefObject<string | null>;
  nextNumberRef: RefObject<number>;
  dispatch: Dispatch<GridSourceAction>;
  setSelectedSource: (sourceId: string | null) => void;
  removeSource: (sourceId: string) => void;
};

export function useGridSources(): GridSourcesResult {
  const [sources, dispatch] = useReducer(gridSourceReducer, []);
  const [selectedSourceId, setSelectedSourceId] = useState<string | null>(null);
  const sourcesRef = useRef<GridSource[]>([]);
  const selectedSourceIdRef = useRef<string | null>(null);
  const nextNumberRef = useRef(1);

  useEffect(() => {
    sourcesRef.current = sources;
  }, [sources]);

  useEffect(() => {
    selectedSourceIdRef.current = selectedSourceId;
  }, [selectedSourceId]);

  const removeSource = useCallback((sourceId: string): void => {
    const wasSelected = selectedSourceIdRef.current === sourceId;
    const remaining = sourcesRef.current.filter((s) => s.id !== sourceId);
    dispatch({ type: GridSourceActionType.REMOVE_SOURCE, payload: { sourceId } });
    if (wasSelected) {
      setSelectedSourceId(remaining[0]?.id ?? null);
    }
  }, []);

  return {
    sources,
    selectedSourceId,
    sourcesRef,
    selectedSourceIdRef,
    nextNumberRef,
    dispatch,
    setSelectedSource: setSelectedSourceId,
    removeSource,
  };
}
