import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import type { ReactElement } from 'react';
import App from './App';
import { ToastProvider } from './context/toast-context';
import { GrpcClientsProvider } from './providers/grpc-clients-provider';

function renderWithProviders(ui: ReactElement) {
  return render(
    <ToastProvider>
      <GrpcClientsProvider>{ui}</GrpcClientsProvider>
    </ToastProvider>
  );
}

vi.mock('./providers/app-services-provider', () => ({
  useAppServices: () => ({
    container: {},
    ready: true,
  }),
}));

vi.mock('./context/dashboard-state-context', () => ({
  useDashboardState: () => ({
    selectedSourceNumber: 1,
    selectedSourceName: 'Lena',
    selectedAccelerator: 'gpu',
    selectedResolution: 'original',
    activeFilters: [],
    processorFilterEpoch: 0,
    setSelectedSource: vi.fn(),
    setAccelerator: vi.fn(),
    setResolution: vi.fn(),
    setActiveFilters: vi.fn(),
  }),
}));

vi.mock('./components/video/VideoGridHost', () => ({
  VideoGridHost: () => <div data-testid="video-grid-host">VideoGridHost</div>,
}));

describe('App', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => { });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('should render VideoGridHost when services are ready', () => {
    renderWithProviders(<App />);
    const grid = screen.getByTestId('video-grid-host');
    expect(grid).toBeInTheDocument();
  });

  it('should show version information in version tooltip', () => {
    renderWithProviders(<App />);
    fireEvent.click(screen.getByTitle('Version Information'));
    expect(screen.getByText('Version Information')).toBeInTheDocument();
  });

  it('should render without console errors', () => {
    renderWithProviders(<App />);
    expect(console.error).not.toHaveBeenCalled();
  });

  it('should have proper shell structure', () => {
    const { container } = renderWithProviders(<App />);

    expect(container.querySelector('.navbar')).toBeInTheDocument();
    expect(container.querySelector('.sidebar')).toBeInTheDocument();
    expect(container.querySelector('.sidebar-content')).toBeInTheDocument();
    expect(container.querySelector('.main-content')).toBeInTheDocument();

    const grid = screen.getByTestId('video-grid-host');
    expect(container.querySelector('.main-content')?.contains(grid)).toBe(true);
  });

  it('should render when wrapped like main.tsx', () => {
    renderWithProviders(<App />);

    expect(screen.getByTestId('video-grid-host')).toBeInTheDocument();
  });
});
