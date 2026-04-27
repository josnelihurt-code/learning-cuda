import { useMemo, type ReactNode } from 'react';
import { createPromiseClient } from '@connectrpc/connect';
import { VideoPlaybackService } from '@/gen/image_processor_service_connect';
import { RemoteManagementService } from '@/gen/remote_management_service_connect';
import { createGrpcConnectTransport } from '@/infrastructure/grpc/create-grpc-transport';
import { ServiceContext } from '@/presentation/context/service-context';

export function GrpcClientsProvider({ children }: { children: ReactNode }) {
  const clients = useMemo(() => {
    const transport = createGrpcConnectTransport();
    return {
      videoPlaybackClient: createPromiseClient(VideoPlaybackService, transport),
      remoteManagementClient: createPromiseClient(RemoteManagementService, transport),
    };
  }, []);

  return <ServiceContext.Provider value={clients}>{children}</ServiceContext.Provider>;
}
