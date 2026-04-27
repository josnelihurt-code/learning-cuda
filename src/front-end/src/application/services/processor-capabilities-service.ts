import { FilterDefinition } from '@/gen/common_pb';
import { GenericFilterDefinition } from '@/gen/image_processor_service_pb';
import { telemetryService } from '@/infrastructure/observability/telemetry-service';
import { logger } from '@/infrastructure/observability/otel-logger';
import {
  Filter,
  createFilterFromGenericDefinition,
} from '@/domain/value-objects/filter-types';
import type { IProcessorCapabilitiesService } from '@/domain/interfaces/i-processor-capabilities-service';
import { controlChannelService } from '@/infrastructure/transport/control-channel-service';

class ProcessorCapabilitiesService implements IProcessorCapabilitiesService {
  private filterDefinitions: FilterDefinition[] = [];
  private filters: Filter[] = [];
  private initPromise: Promise<void> | null = null;
  private genericFilters: GenericFilterDefinition[] = [];
  private genericFiltersLoaded = false;
  private filterListeners = new Set<() => void>();

  async initialize(): Promise<void> {
    if (this.initPromise) {
      return this.initPromise;
    }
    this.initPromise = this.fetchGenericFilters();
    return this.initPromise;
  }

  getFilters(): Filter[] {
    return this.filters.map((f) => ({ ...f }));
  }

  getFilterDefinitions(): FilterDefinition[] {
    return this.filterDefinitions;
  }

  isInitialized(): boolean {
    return this.filters.length > 0;
  }

  isGRPCAvailable(): boolean {
    return this.genericFiltersLoaded;
  }

  getGenericFilters(): GenericFilterDefinition[] {
    return this.genericFilters.slice();
  }

  addFiltersUpdatedListener(listener: () => void): void {
    this.filterListeners.add(listener);
  }

  removeFiltersUpdatedListener(listener: () => void): void {
    this.filterListeners.delete(listener);
  }

  private async fetchGenericFilters(): Promise<void> {
    if (this.genericFiltersLoaded) {
      return;
    }

    return telemetryService.withSpanAsync(
      'ProcessorCapabilitiesService.fetchGenericFilters',
      {
        'rpc.service': 'ControlChannel',
        'rpc.method': 'listFilters',
      },
      async (span) => {
        try {
          span?.addEvent('Awaiting control channel and fetching filter definitions');
          const response = await controlChannelService.listFilters();
          this.genericFilters = response.filters ?? [];
          this.genericFiltersLoaded = true;

          if (this.genericFilters.length > 0) {
            this.filters = this.genericFilters.map(createFilterFromGenericDefinition);
            this.notifyFilterListeners();
          }

          span?.setAttribute('filters.count', this.genericFilters.length);
          logger.info('Filters loaded over control channel', {
            'filters.count': this.genericFilters.length,
            'filters.api_version': response.apiVersion,
          });
        } catch (error) {
          span?.setAttribute('error', true);
          logger.error('Failed to load filters', {
            'error.message': error instanceof Error ? error.message : String(error),
          });
          this.genericFilters = [];
          this.genericFiltersLoaded = false;
        } finally {
          this.initPromise = null;
        }
      }
    );
  }

  private notifyFilterListeners(): void {
    this.filterListeners.forEach((listener) => {
      try {
        listener();
      } catch (error) {
        logger.warn('Filter listener threw an error', {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
    });
  }
}

export const processorCapabilitiesService = new ProcessorCapabilitiesService();
