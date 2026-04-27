import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import type { ReactElement } from 'react';
import { ServiceContext, type GrpcClients } from '@/presentation/context/service-context';

export function renderWithService(
  ui: ReactElement,
  overrides?: Partial<GrpcClients>
): { root: Root; unmount: () => void } {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  const value: GrpcClients = {
    videoPlaybackClient:
      overrides?.videoPlaybackClient ??
      ({} as GrpcClients['videoPlaybackClient']),
    remoteManagementClient:
      overrides?.remoteManagementClient ??
      ({} as GrpcClients['remoteManagementClient']),
  };

  act(() => {
    root.render(<ServiceContext.Provider value={value}>{ui}</ServiceContext.Provider>);
  });

  return {
    root,
    unmount: () => {
      act(() => {
        root.unmount();
      });
      container.remove();
    },
  };
}
