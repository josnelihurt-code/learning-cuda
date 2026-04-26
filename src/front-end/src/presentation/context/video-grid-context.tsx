import { createContext, useContext, type ReactNode } from 'react';
import type { ActiveFilterState } from '@/presentation/components/filters/FilterPanel';
import type { GridSource } from '@/presentation/utils/grid-source';
import type { SourceType } from '@/presentation/components/video/SourceDetailsBadge';

type CameraFramePayload = {
  base64data: string;
  width: number;
  height: number;
  timestamp: number;
};

type SelectedSourceDetails = {
  type: SourceType;
  fps?: number;
  width?: number;
  height?: number;
  metrics?: Record<string, string>;
} | null;

type TransportStatus = {
  state: 'connected' | 'disconnected' | 'connecting' | 'error';
  lastRequest: string | null;
  lastRequestTime: Date | null;
};

type VideoGridContextValue = {
  sources: GridSource[];
  selectedSourceId: string | null;
  selectedSourceDetails: SelectedSourceDetails;
  activeTransportService: { getConnectionStatus: () => TransportStatus } | null;
  onSelectSource: (sourceId: string) => void;
  onCloseSource: (sourceId: string) => void;
  onChangeImageRequest: (sourceId: string) => void;
  onCameraFrame: (sourceId: string, payload: CameraFramePayload) => void;
  onCameraStreamReady: (sourceId: string, stream: MediaStream) => void;
  onCameraStatus: (status: string, type: 'success' | 'error' | 'warning' | 'inactive') => void;
  onCameraError: (title: string, message: string) => void;
  onSourceFpsUpdate: (sourceId: string, fps: number) => void;
  onSourceResolutionUpdate: (sourceId: string, width: number, height: number) => void;
};

const VideoGridContext = createContext<VideoGridContextValue | null>(null);

type VideoGridProviderProps = {
  value: VideoGridContextValue;
  children: ReactNode;
};

export function VideoGridProvider({ value, children }: VideoGridProviderProps): ReactNode {
  return (
    <VideoGridContext.Provider value={value}>
      {children}
    </VideoGridContext.Provider>
  );
}

export function useVideoGridContext(): VideoGridContextValue {
  const context = useContext(VideoGridContext);
  if (context) {
    return context;
  }
  throw new Error('useVideoGridContext must be used within VideoGridProvider');
}

export type {
  ActiveFilterState,
  CameraFramePayload,
  SelectedSourceDetails,
  TransportStatus,
  VideoGridContextValue,
};
