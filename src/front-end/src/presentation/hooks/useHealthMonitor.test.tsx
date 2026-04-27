import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { useEffect } from 'react';
import { act } from 'react';
import {
  AcceleratorHealthStatus,
  CheckAcceleratorHealthResponse,
} from '@/gen/remote_management_service_pb';
import { useHealthMonitor } from '@/presentation/hooks/useHealthMonitor';
import { renderWithService } from '@/presentation/test-utils/render-with-service';
import type { GrpcClients } from '@/presentation/context/service-context';

function setVisibility(state: DocumentVisibilityState): void {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    value: state,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('useHealthMonitor', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility('visible');
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('flips isHealthy after a poll cycle when the RPC status changes', async () => {
    const checkAcceleratorHealth = vi
      .fn()
      .mockResolvedValueOnce(
        new CheckAcceleratorHealthResponse({
          status: AcceleratorHealthStatus.HEALTHY,
        })
      )
      .mockResolvedValue(
        new CheckAcceleratorHealthResponse({
          status: AcceleratorHealthStatus.UNHEALTHY,
        })
      );

    const healthyFlags: boolean[] = [];

    function Probe(): React.ReactNode {
      const { isHealthy } = useHealthMonitor({ pollIntervalMs: 5000 });
      useEffect(() => {
        healthyFlags.push(isHealthy);
      }, [isHealthy]);
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: {} as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {
        checkAcceleratorHealth,
      } as unknown as GrpcClients['remoteManagementClient'],
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(checkAcceleratorHealth).toHaveBeenCalledTimes(1);
      });
    });

    expect(healthyFlags.some((h) => h === true)).toBe(true);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(checkAcceleratorHealth).toHaveBeenCalledTimes(2);
      });
    });

    expect(healthyFlags.at(-1)).toBe(false);
    unmount();
  });

  it('does not schedule checks while hidden and resumes after visible', async () => {
    const checkAcceleratorHealth = vi.fn().mockResolvedValue(
      new CheckAcceleratorHealthResponse({
        status: AcceleratorHealthStatus.HEALTHY,
      })
    );

    function Probe(): React.ReactNode {
      useHealthMonitor({ pollIntervalMs: 1000 });
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: {} as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {
        checkAcceleratorHealth,
      } as unknown as GrpcClients['remoteManagementClient'],
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(checkAcceleratorHealth.mock.calls.length).toBeGreaterThanOrEqual(
          1
        );
      });
    });

    const afterInitial = checkAcceleratorHealth.mock.calls.length;

    await act(async () => {
      setVisibility('hidden');
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });

    expect(checkAcceleratorHealth.mock.calls.length).toBe(afterInitial);

    await act(async () => {
      setVisibility('visible');
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(checkAcceleratorHealth.mock.calls.length).toBeGreaterThan(
          afterInitial
        );
      });
    });

    const afterVisible = checkAcceleratorHealth.mock.calls.length;

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(checkAcceleratorHealth.mock.calls.length).toBeGreaterThan(
          afterVisible
        );
      });
    });

    unmount();
  });
});
