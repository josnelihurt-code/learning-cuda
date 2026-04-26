import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { VideoSourceCard } from './VideoSourceCard';
import { VideoGridProvider, type VideoGridContextValue } from '@/presentation/context/video-grid-context';
import type { GridSource } from '@/presentation/utils/grid-source';

function makeSource(overrides: Partial<GridSource> = {}): GridSource {
  return {
    id: 'source-1',
    number: 1,
    name: 'Lena',
    type: 'static',
    imagePath: '/image.png',
    originalImageSrc: '/image.png',
    currentImageSrc: '/image.png',
    transport: null,
    remoteStream: null,
    sessionId: null,
    sessionMode: 'frame-processing',
    filters: [],
    resolution: 'original',
    accelerator: 'gpu',
    detections: [],
    detectionImageWidth: 0,
    detectionImageHeight: 0,
    fps: 0,
    displayWidth: 0,
    displayHeight: 0,
    connected: false,
    metrics: {},
    ...overrides,
  };
}

function makeContextValue(overrides: Partial<VideoGridContextValue> = {}): VideoGridContextValue {
  return {
    sources: [],
    selectedSourceId: null,
    selectedSourceDetails: null,
    activeTransportService: null,
    onSelectSource: vi.fn(),
    onCloseSource: vi.fn(),
    onChangeImageRequest: vi.fn(),
    onCameraFrame: vi.fn(),
    onCameraStreamReady: vi.fn(),
    onCameraStatus: vi.fn(),
    onCameraError: vi.fn(),
    onSourceFpsUpdate: vi.fn(),
    onSourceResolutionUpdate: vi.fn(),
    ...overrides,
  };
}

function renderCard(source: GridSource, contextOverrides: Partial<VideoGridContextValue> = {}) {
  const context = makeContextValue(contextOverrides);
  render(
    <VideoGridProvider value={context}>
      <VideoSourceCard source={source} />
    </VideoGridProvider>
  );
  return context;
}

describe('VideoSourceCard', () => {
  it('calls select and close handlers', () => {
    const context = renderCard(makeSource());

    fireEvent.click(screen.getByTestId('source-card-1'));
    expect(context.onSelectSource).toHaveBeenCalledWith('source-1');

    fireEvent.click(screen.getByTestId('source-close-button'));
    expect(context.onCloseSource).toHaveBeenCalledWith('source-1');
  });

  it('triggers change image for static source only', () => {
    const staticContext = renderCard(makeSource({ type: 'static' }));
    fireEvent.click(screen.getByTestId('change-image-button'));
    expect(staticContext.onChangeImageRequest).toHaveBeenCalledWith('source-1');
  });

  it('hides change image button for non-static sources', () => {
    renderCard(makeSource({ type: 'camera' }));
    expect(screen.queryByTestId('change-image-button')).not.toBeInTheDocument();
  });

  it('shows expanded source details by default and toggles on click for video source', () => {
    renderCard(
      makeSource({
        id: 'video-1',
        number: 2,
        name: 'Sample video',
        type: 'video',
        imagePath: '/video-preview.png',
        originalImageSrc: '/video-preview.png',
        currentImageSrc: '/video-preview.png',
        fps: 23.456,
        displayWidth: 1280,
        displayHeight: 720,
      })
    );

    const sourceDetails = screen.getByTestId('source-fps-2');
    expect(sourceDetails).toHaveAttribute('aria-expanded', 'true');
    expect(sourceDetails).toHaveTextContent('fps:');
    expect(sourceDetails).toHaveTextContent('23.5');
    expect(sourceDetails).toHaveTextContent('1280x720');
    expect(sourceDetails).toHaveTextContent('Video');

    fireEvent.click(sourceDetails);
    expect(sourceDetails).toHaveAttribute('aria-expanded', 'false');
  });

  it('shows source details with camera-specific labels', () => {
    renderCard(
      makeSource({
        id: 'camera-1',
        number: 3,
        name: 'Camera',
        type: 'camera',
        imagePath: '',
        originalImageSrc: '',
        currentImageSrc: '',
        fps: 30,
        displayWidth: 640,
        displayHeight: 480,
      })
    );

    const sourceDetails = screen.getByTestId('source-fps-3');
    expect(sourceDetails).toHaveTextContent('fps:');
    expect(sourceDetails).toHaveTextContent('30.0');
    expect(sourceDetails).toHaveTextContent('640x480');
    expect(sourceDetails).toHaveTextContent('Webcam');
  });

  it('shows resolution fallback when unavailable', () => {
    renderCard(
      makeSource({
        id: 'video-2',
        number: 5,
        name: 'Sample video fallback',
        type: 'video',
        imagePath: '/video-preview.png',
        originalImageSrc: '/video-preview.png',
        currentImageSrc: '/video-preview.png',
        fps: 12,
      })
    );

    const sourceDetails = screen.getByTestId('source-fps-5');
    expect(sourceDetails).toHaveTextContent('fps:');
    expect(sourceDetails).toHaveTextContent('12.0');
    expect(sourceDetails).toHaveTextContent('--');
  });

  it('shows source details for static source with static labels', () => {
    renderCard(
      makeSource({
        id: 'static-1',
        number: 4,
        name: 'Static',
        type: 'static',
        imagePath: '/img.png',
        originalImageSrc: '/img.png',
        currentImageSrc: '/img.png',
        fps: 30,
      })
    );

    const sourceDetails = screen.getByTestId('source-fps-4');
    expect(sourceDetails).not.toHaveTextContent('fps:');
    expect(sourceDetails).not.toHaveTextContent('30.0');
    expect(sourceDetails).toHaveTextContent('Static');
  });
});
