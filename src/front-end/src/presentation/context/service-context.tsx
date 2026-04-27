import { createContext, useContext } from 'react';
import type { PromiseClient } from '@connectrpc/connect';
import { VideoPlaybackService } from '@/gen/image_processor_service_connect';
import { RemoteManagementService } from '@/gen/remote_management_service_connect';

export type GrpcClients = {
  videoPlaybackClient: PromiseClient<typeof VideoPlaybackService>;
  remoteManagementClient: PromiseClient<typeof RemoteManagementService>;
};

export const ServiceContext = createContext<GrpcClients | null>(null);

export function useServiceContext(): GrpcClients {
  const ctx = useContext(ServiceContext);
  if (!ctx) {
    throw new Error('useServiceContext must be used within GrpcClientsProvider or ServiceContext.Provider');
  }
  return ctx;
}
