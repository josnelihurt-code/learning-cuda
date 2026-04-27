import { describe, it, expect, vi, afterEach } from 'vitest';
import { useEffect } from 'react';
import { act } from 'react';
import { ConnectError, Code } from '@connectrpc/connect';
import { ListFiltersResponse } from '@/gen/image_processor_service_pb';
import { useAsyncGRPC } from '@/presentation/hooks/useAsyncGRPC';
import { renderWithService } from '@/presentation/test-utils/render-with-service';
import type { GrpcClients } from '@/presentation/context/service-context';

afterEach(() => {
  document.body.replaceChildren();
});

describe('useAsyncGRPC', () => {
  it('transitions loading to data on success', async () => {
    const listFilters = vi.fn().mockResolvedValue(new ListFiltersResponse({}));
    const snapshots: { loading: boolean; data?: unknown }[] = [];

    function Probe(): React.ReactNode {
      const { data, loading } = useAsyncGRPC(
        async (clients, { signal }) =>
          clients.videoPlaybackClient.startVideoPlayback({}, { signal }),
        []
      );
      snapshots.push({ loading, data });
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: { startVideoPlayback: listFilters } as unknown as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {} as GrpcClients['remoteManagementClient'],
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(listFilters).toHaveBeenCalled();
      });
    });

    await act(async () => {
      await Promise.resolve();
    });

    expect(listFilters).toHaveBeenCalled();
    const last = snapshots[snapshots.length - 1];
    expect(last?.loading).toBe(false);
    expect(last?.data).toBeDefined();
    unmount();
  });

  it('maps ConnectError to stable error shape', async () => {
    const listFilters = vi
      .fn()
      .mockRejectedValue(new ConnectError('rpc failed', Code.Unavailable));
    const errors: { message: string; code?: string }[] = [];

    function Probe(): React.ReactNode {
      const { error, loading } = useAsyncGRPC(
        async (clients, { signal }) =>
          clients.videoPlaybackClient.startVideoPlayback({}, { signal }),
        []
      );
      useEffect(() => {
        if (!loading && error) {
          errors.push(error);
        }
      }, [loading, error]);
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: { startVideoPlayback: listFilters } as unknown as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {} as GrpcClients['remoteManagementClient'],
    });

    await vi.waitFor(
      () => {
        expect(errors.length).toBeGreaterThan(0);
      },
      { timeout: 3000 }
    );

    expect(errors[0]?.message).toBeDefined();
    expect(errors[0]?.code).toBe(String(Code.Unavailable));
    unmount();
  });

  it('refetch runs executor again', async () => {
    const listFilters = vi.fn().mockResolvedValue(new ListFiltersResponse({}));
    let refetch: (() => void) | undefined;

    function Probe(): React.ReactNode {
      const grpc = useAsyncGRPC(
        async (clients, { signal }) =>
          clients.videoPlaybackClient.startVideoPlayback({}, { signal }),
        []
      );
      refetch = grpc.refetch;
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: { startVideoPlayback: listFilters } as unknown as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {} as GrpcClients['remoteManagementClient'],
    });

    await act(async () => {
      await vi.waitFor(() => expect(listFilters).toHaveBeenCalledTimes(1));
    });

    await act(async () => {
      refetch?.();
    });

    await act(async () => {
      await vi.waitFor(() => expect(listFilters).toHaveBeenCalledTimes(2));
    });

    unmount();
  });
});
