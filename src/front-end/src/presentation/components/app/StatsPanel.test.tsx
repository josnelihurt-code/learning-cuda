import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { StatsPanel } from './StatsPanel';

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

describe('StatsPanel', () => {
  it('renders source details always expanded and hides legacy time/frames labels', () => {
    render(
      <StatsPanel
        selectedSource={{
          type: 'camera',
          fps: 28.4,
          width: 1920,
          height: 1080,
        }}
        transportService={{
          getConnectionStatus: () => ({
            state: 'connected',
            lastRequest: 'frame.push',
            lastRequestTime: new Date(),
          }),
        }}
      />
    );

    const details = screen.getByTestId('stats-panel-source-details');
    expect(details).toHaveAttribute('aria-expanded', 'true');
    expect(details).not.toHaveTextContent('fps:');
    expect(details).toHaveTextContent('1920x1080');
    expect(screen.queryByText(/Time:/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Frames:/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Active/i)).not.toBeInTheDocument();
  });
});
