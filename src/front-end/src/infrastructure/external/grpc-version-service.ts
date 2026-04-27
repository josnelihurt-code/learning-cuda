import { GetVersionInfoRequest } from '@/gen/image_processor_service_pb';
import { TraceContext } from '@/gen/common_pb';
import { telemetryService } from '@/infrastructure/observability/telemetry-service';
import { logger } from '@/infrastructure/observability/otel-logger';
import { controlChannelService } from '@/infrastructure/transport/control-channel-service';

class GrpcVersionService {
  async getVersionInfo() {
    return telemetryService.withSpanAsync(
      'GrpcVersionService.getVersionInfo',
      {
        'rpc.service': 'ControlChannel',
        'rpc.method': 'GetVersionInfo',
      },
      async (span) => {
        try {
          span?.addEvent('Sending GetVersion request over WebRTC control channel');

          const traceHeaders = telemetryService.getTraceHeaders();
          const traceContext = traceHeaders['traceparent']
            ? new TraceContext({ traceparent: traceHeaders['traceparent'] })
            : undefined;

          const request = new GetVersionInfoRequest({
            apiVersion: '2.1.0',
            traceContext,
          });

          const versionInfo = await controlChannelService.getVersion(request);

          if (versionInfo.code !== 0) {
            span?.setAttribute('error', true);
            span?.setAttribute('error.message', versionInfo.message);
            throw new Error(`Version error: ${versionInfo.message}`);
          }

          span?.setAttribute('version.server', versionInfo.serverVersion);
          span?.setAttribute('version.library', versionInfo.libraryVersion);
          span?.setAttribute('version.build_date', versionInfo.buildDate);
          span?.setAttribute('version.build_commit', versionInfo.buildCommit);

          logger.info('Accelerator version info retrieved', {
            'version.server': versionInfo.serverVersion,
            'version.library': versionInfo.libraryVersion,
            'version.build_date': versionInfo.buildDate,
          });

          return versionInfo;
        } catch (error) {
          span?.setAttribute('error', true);
          span?.setAttribute('error.message', error instanceof Error ? error.message : String(error));
          logger.error('Failed to get accelerator version info', {
            'error.message': error instanceof Error ? error.message : String(error),
          });
          throw error;
        }
      }
    );
  }
}

export const grpcVersionService = new GrpcVersionService();
