import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { StatsPanel } from './StatsPanel';
import { VideoGridProvider, type VideoGridContextValue } from '@/presentation/context/video-grid-context';

vi.mock('@/infrastructure/connection/grpc-connection-service', () => ({
  grpcConnectionService: {
    getConnectionStatus: () => ({
      state: 'connected',
      lastRequest: 'grpc.health',
      lastRequestTime: new Date(),
    }),
  },
}));

vi.mock('@/infrastructure/connection/webrtc-service', () => ({
  webrtcService: {
    getConnectionStatus: () => ({
      state: 'connected',
      lastRequest: 'webrtc.offer',
      lastRequestTime: new Date(),
    }),
  },
}));

function makeContextValue(overrides: Partial<VideoGridContextValue> = {}): VideoGridContextValue {
  return {
    sources: [],
    selectedSourceId: null,
    selectedSourceDetails: {
      type: 'camera',
      fps: 28.4,
      width: 1920,
      height: 1080,
    },
    activeTransportService: {
      getConnectionStatus: () => ({
        state: 'connected',
        lastRequest: 'frame.push',
        lastRequestTime: new Date(),
      }),
    },
    onSelectSource: vi.fn(),
    onCloseSource: vi.fn(),
    onChangeImageRequest: vi.fn(),
    onCameraFrame: vi.fn(),
    onCameraStreamReady: vi.fn(),
    onCameraStatus: vi.fn(),
    onCameraError: vi.fn(),
    onSourceFpsUpdate: vi.fn(),
    onSourceResolutionUpdate: vi.fn(),
    ...overrides,
  };
}

describe('StatsPanel', () => {
  it('renders source details always expanded and hides legacy time/frames labels', () => {
    render(
      <VideoGridProvider value={makeContextValue()}>
        <StatsPanel />
      </VideoGridProvider>
    );

    const details = screen.getByTestId('stats-panel-source-details');
    expect(details).toHaveAttribute('aria-expanded', 'true');
    expect(details).toHaveTextContent('1920x1080');
    expect(screen.queryByText(/Time:/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Frames:/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Active/i)).not.toBeInTheDocument();
  });
});
