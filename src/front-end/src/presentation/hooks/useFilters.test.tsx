import { describe, it, expect, vi, afterEach } from 'vitest';
import { useEffect } from 'react';
import { act } from 'react';
import {
  GenericFilterDefinition,
  ListFiltersResponse,
} from '@/gen/image_processor_service_pb';
import { useFilters } from '@/presentation/hooks/useFilters';
import { renderWithService } from '@/presentation/test-utils/render-with-service';
import type { GrpcClients } from '@/presentation/context/service-context';
import { controlChannelService } from '@/infrastructure/transport/control-channel-service';

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe('useFilters', () => {
  it('loads filters via control channel, then refetch increases call count', async () => {
    const listFiltersSpy = vi
      .spyOn(controlChannelService, 'listFilters')
      .mockResolvedValue(
        new ListFiltersResponse({
          filters: [new GenericFilterDefinition({ id: 'blur', name: 'Blur' })],
        })
      );

    const snapshots: {
      loading: boolean;
      filtersLen: number;
      firstId?: string;
    }[] = [];
    let refetchFn: (() => void) | undefined;

    function Probe(): React.ReactNode {
      const { filters, loading, refetch } = useFilters();
      refetchFn = refetch;
      useEffect(() => {
        snapshots.push({
          loading,
          filtersLen: filters.length,
          firstId: filters[0]?.id,
        });
      }, [loading, filters]);
      return null;
    }

    const { unmount } = renderWithService(<Probe />, {
      videoPlaybackClient: {} as GrpcClients['videoPlaybackClient'],
      remoteManagementClient: {} as GrpcClients['remoteManagementClient'],
    });

    await act(async () => {
      await vi.waitFor(() => {
        expect(listFiltersSpy).toHaveBeenCalled();
      });
    });

    await act(async () => {
      await Promise.resolve();
    });

    expect(listFiltersSpy).toHaveBeenCalledTimes(1);
    const done = snapshots.filter((s) => !s.loading).pop();
    expect(done?.filtersLen).toBe(1);
    expect(done?.firstId).toBe('blur');

    await act(async () => {
      refetchFn?.();
      await Promise.resolve();
    });

    await act(async () => {
      await Promise.resolve();
    });

    expect(listFiltersSpy).toHaveBeenCalledTimes(2);
    unmount();
  });
});
