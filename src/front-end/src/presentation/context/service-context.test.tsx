import { describe, it, expect, vi, afterEach } from 'vitest';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import { useServiceContext } from '@/presentation/context/service-context';
import { renderWithService } from '@/presentation/test-utils/render-with-service';
import type { GrpcClients } from '@/presentation/context/service-context';

function Consumer(): React.ReactNode {
  useServiceContext();
  return <span>has-clients</span>;
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('ServiceContext / useServiceContext', () => {
  it('throws when used outside provider', () => {
    function Bad(): React.ReactNode {
      useServiceContext();
      return null;
    }
    const div = document.createElement('div');
    document.body.appendChild(div);
    const root = createRoot(div);
    expect(() => {
      act(() => root.render(<Bad />));
    }).toThrow(/useServiceContext/);
    act(() => root.unmount());
    div.remove();
  });

  it('returns clients when wrapped with renderWithService', () => {
    const videoPlaybackClient = {
      listFilters: vi.fn(),
    } as unknown as GrpcClients['videoPlaybackClient'];
    const remoteManagementClient = {} as GrpcClients['remoteManagementClient'];

    const { unmount } = renderWithService(<Consumer />, {
      videoPlaybackClient,
      remoteManagementClient,
    });

    expect(document.body.textContent).toContain('has-clients');
    unmount();
  });
});
