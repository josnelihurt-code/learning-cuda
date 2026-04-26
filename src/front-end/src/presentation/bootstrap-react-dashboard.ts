import { container } from '@/application/di';
import { acceleratorHealthMonitor } from '@/infrastructure/external/accelerator-health-monitor';
import { systemInfoService } from '@/infrastructure/external/system-info-service';
import { featureFlagsService } from '@/infrastructure/external/feature-flags-service';

declare global {
  interface Window {
    __reactGrpcStatusModal?: {
      isOpen: () => boolean;
    };
  }
}

let bootstrapPromise: Promise<void> | null = null;
let beforeUnloadAttached = false;
const BOOTSTRAP_STEP_TIMEOUT_MS = 8000;

function attachBeforeUnload(): void {
  if (beforeUnloadAttached) {
    return;
  }
  beforeUnloadAttached = true;
  window.addEventListener('beforeunload', () => {
    const webrtcService = container.getWebRTCService();
    const logger = container.getLogger();
    const activeSessions = webrtcService.getActiveSessions();
    activeSessions.forEach((session) => {
      const sessionId = session.getId();
      webrtcService.stopHeartbeat(sessionId);
      // Use sendBeacon: a regular fetch is cancelled by the browser during
      // unload, so the C++ accelerator never sees CloseSession and holds
      // encoder/CUDA-pool resources until ICE consent expires (~30s),
      // blocking the next page load from connecting.
      const queued = webrtcService.closeSessionBeacon(sessionId);
      if (!queued) {
        logger.warn('CloseSession beacon was not queued on beforeunload', {
          'session.id': sessionId,
        });
      }
    });
    acceleratorHealthMonitor.stopMonitoring();
    container.getLogger().shutdown();
  });
}

async function runBootstrap(): Promise<void> {
  const streamConfigService = container.getConfigService();
  const telemetryService = container.getTelemetryService();
  const logger = container.getLogger();
  const inputSourceService = container.getInputSourceService();
  const processorCapabilitiesService = container.getProcessorCapabilitiesService();
  const toolsService = container.getToolsService();
  const videoService = container.getVideoService();
  const webrtcService = container.getWebRTCService();

  logger.info('Initializing dashboard (React)...');

  const withTimeout = async <T>(promise: Promise<T>, stepName: string, fallback: T): Promise<T> => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    try {
      return await Promise.race<T>([
        promise,
        new Promise<T>((resolve) => {
          timer = setTimeout(() => {
            logger.warn(`Bootstrap step timed out, continuing in degraded mode: ${stepName}`, {
              'bootstrap.step': stepName,
              'bootstrap.timeout_ms': BOOTSTRAP_STEP_TIMEOUT_MS,
            });
            resolve(fallback);
          }, BOOTSTRAP_STEP_TIMEOUT_MS);
        }),
      ]);
    } finally {
      if (timer) {
        clearTimeout(timer);
      }
    }
  };

  const observabilityEnabled = await withTimeout(
    featureFlagsService.isFeatureEnabled('observability_enabled'),
    'featureFlagsService.isFeatureEnabled(observability_enabled)',
    false
  );

  const coreServicesResults = await Promise.allSettled([
    withTimeout(
      telemetryService.initialize(observabilityEnabled),
      'telemetryService.initialize',
      undefined
    ),
    withTimeout(
      streamConfigService.initialize(),
      'streamConfigService.initialize',
      undefined
    ),
  ]);

  coreServicesResults.forEach((result, index) => {
    if (result.status === 'rejected') {
      const serviceName = index === 0 ? 'Telemetry' : 'Config';
      logger.error(`${serviceName} service failed to initialize`, {
        'error.message':
          result.reason instanceof Error ? result.reason.message : String(result.reason),
      });
      logger.error(`${serviceName} initialization warning surfaced to user`, {
        'service.name': serviceName,
      });
    }
  });

  logger.initialize(streamConfigService.getLogLevel(), streamConfigService.getConsoleLogging(), undefined, observabilityEnabled);

  const buildVersion = typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev';
  const buildBranch = typeof __APP_BRANCH__ !== 'undefined' ? __APP_BRANCH__ : 'unknown';
  const buildTime = typeof __BUILD_TIME__ !== 'undefined' ? __BUILD_TIME__ : new Date().toISOString();

  logger.info('Frontend starting', {
    version: buildVersion,
    git_commit: buildVersion,
    branch: buildBranch,
    build_time: buildTime,
  });

  try {
    await systemInfoService.initialize();
    const systemInfo = await systemInfoService.getSystemInfo();
    if (systemInfo.environment) {
      logger.setEnvironment(systemInfo.environment);
    }
  } catch (error) {
    logger.warn('Failed to load system info for environment', {
      'error.message': error instanceof Error ? error.message : String(error),
    });
  }

  const dataServicesResults = await Promise.allSettled([
    withTimeout(
      inputSourceService.initialize(),
      'inputSourceService.initialize',
      undefined
    ),
    withTimeout(
      processorCapabilitiesService.initialize(),
      'processorCapabilitiesService.initialize',
      undefined
    ),
    withTimeout(
      toolsService.initialize(),
      'toolsService.initialize',
      undefined
    ),
    withTimeout(
      videoService.initialize(),
      'videoService.initialize',
      undefined
    ),
    withTimeout(
      webrtcService.initialize(),
      'webrtcService.initialize',
      undefined
    ),
  ]);

  dataServicesResults.forEach((result, index) => {
    if (result.status === 'rejected') {
      const serviceNames = [
        'Input Source',
        'Processor Capabilities',
        'Tools',
        'Video',
        'WebRTC',
      ];
      const serviceName = serviceNames[index];
      logger.error(`${serviceName} service failed to initialize`, {
        'error.message':
          result.reason instanceof Error ? result.reason.message : String(result.reason),
      });
      logger.error(`${serviceName} data loading warning surfaced to user`, {
        'service.name': serviceName,
      });
    }
  });

  acceleratorHealthMonitor.startMonitoring(
    () => {
      logger.warn('Accelerator health check detected unhealthy status, opening modal');
      document.dispatchEvent(new CustomEvent('accelerator-unhealthy'));
    },
    () => Boolean(window.__grpcStatusModal?.isOpen())
  );
  logger.info('Accelerator health monitoring started');

  attachBeforeUnload();
  logger.info('Dashboard (React) services initialized');
}

export function ensureReactDashboardBootstrap(): Promise<void> {
  if (!bootstrapPromise) {
    bootstrapPromise = runBootstrap();
  }
  return bootstrapPromise;
}
