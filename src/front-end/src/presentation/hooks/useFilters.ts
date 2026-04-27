import { useCallback, useEffect, useRef, useState } from 'react';
import { type GenericFilterDefinition } from '@/gen/image_processor_service_pb';
import { controlChannelService } from '@/infrastructure/transport/control-channel-service';
import type { GrpcAsyncError } from './useAsyncGRPC';

export function useFilters() {
  const [filters, setFilters] = useState<GenericFilterDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<GrpcAsyncError | null>(null);
  const requestGenRef = useRef(0);

  const refetch = useCallback(() => {
    const gen = ++requestGenRef.current;
    setLoading(true);
    setError(null);

    void (async () => {
      try {
        const response = await controlChannelService.listFilters();
        if (gen !== requestGenRef.current) return;
        setFilters(response.filters);
      } catch (e) {
        if (gen !== requestGenRef.current) return;
        const message = e instanceof Error ? e.message : String(e);
        setError({ message, code: 'UNKNOWN' });
      } finally {
        if (gen === requestGenRef.current) {
          setLoading(false);
        }
      }
    })();
  }, []);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { filters, loading, error, refetch };
}
