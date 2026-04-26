import type { WebRTCSession, WebRTCSessionMode } from '@/domain/value-objects/webrtc-session';
import type { ProcessImageRequest } from '@/gen/image_processor_service_pb';

export type CreateWebRTCSessionOptions = {
  mode?: WebRTCSessionMode;
  useDataChannel?: boolean;
  localStream?: MediaStream | null;
  onRemoteStream?: (stream: MediaStream) => void;
};

export interface IWebRTCService {
  initialize(): Promise<void>;
  isInitialized(): boolean;
  createPeerConnection(config?: RTCConfiguration): RTCPeerConnection | null;
  createDataChannel(peerConnection: RTCPeerConnection, label: string, options?: RTCDataChannelInit): RTCDataChannel | null;
  getUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream>;
  isSupported(): boolean;
  setupPingChannel(sessionId: string): Promise<void>;
  createSession(sourceId: string, options?: CreateWebRTCSessionOptions): Promise<WebRTCSession>;
  getDataChannel(sessionId: string): RTCDataChannel | null;
  getStatsDataChannel(sessionId: string): RTCDataChannel | null;
  ensureStatsDataChannel(sessionId: string): RTCDataChannel | null;
  getPeerConnection(sessionId: string): RTCPeerConnection | null;
  replaceLocalVideoTrack(sessionId: string, track: MediaStreamTrack | null): Promise<boolean>;
  waitForTransportReady(sessionId: string, timeoutMs?: number): Promise<RTCDataChannel>;
  sendControlRequest(sessionId: string, request: ProcessImageRequest): void;
  closeSession(sessionId: string): Promise<void>;
  closeSessionBeacon(sessionId: string): boolean;
  startHeartbeat(sessionId: string, intervalMs: number): void;
  stopHeartbeat(sessionId: string): void;
  getActiveSessions(): WebRTCSession[];
}
