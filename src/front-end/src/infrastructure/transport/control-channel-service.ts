import {
  ControlRequest,
  ControlResponse,
  GetVersionInfoRequest,
  GetVersionInfoResponse,
  ListFiltersRequest,
  ListFiltersResponse,
} from '@/gen/image_processor_service_pb';
import { TraceContext } from '@/gen/common_pb';
import { webrtcService } from '@/infrastructure/connection/webrtc-service';
import { logger } from '@/infrastructure/observability/otel-logger';
import { ChunkReassembler, nextMessageId, packMessage } from '@/infrastructure/transport/data-channel-framing';

const REQUEST_TIMEOUT_MS = 15_000;

type Pending = {
  resolve: (response: ControlResponse) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

function newRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `req-${Date.now()}-${Math.floor(Math.random() * 1e9).toString(16)}`;
}

// ControlChannelService routes ListFilters / GetVersionInfo requests to the C++
// accelerator over the per-session "control" WebRTC data channel. It tracks
// the currently open control channel: if no peer connection exists yet, the
// caller's promise is queued and resolved as soon as a session opens. If the
// channel closes (e.g. browser refresh, peer drop), pending requests are
// rejected so the caller can retry on the next connection.
class ControlChannelService {
  private channel: RTCDataChannel | null = null;
  private reassembler = new ChunkReassembler();
  private pending = new Map<string, Pending>();
  private queue: Array<() => void> = [];

  constructor() {
    webrtcService.addControlChannelListener((sessionId, channel) => {
      this.onChannelChange(sessionId, channel);
    });
    const existing = webrtcService.getAnyOpenControlDataChannel();
    if (existing) {
      this.attachChannel(existing);
    }
  }

  isReady(): boolean {
    return this.channel !== null && this.channel.readyState === 'open';
  }

  async listFilters(req: ListFiltersRequest = new ListFiltersRequest({})): Promise<ListFiltersResponse> {
    const response = await this.sendRequest(new ControlRequest({
      payload: { case: 'listFilters', value: req },
    }));
    if (response.payload.case === 'error') {
      throw new Error(`ListFilters failed: ${response.payload.value.message}`);
    }
    if (response.payload.case !== 'listFilters') {
      throw new Error(`ListFilters: unexpected response case ${String(response.payload.case)}`);
    }
    return response.payload.value;
  }

  async getVersion(req: GetVersionInfoRequest = new GetVersionInfoRequest({ apiVersion: '2.1.0' })): Promise<GetVersionInfoResponse> {
    const response = await this.sendRequest(new ControlRequest({
      payload: { case: 'getVersion', value: req },
    }));
    if (response.payload.case === 'error') {
      throw new Error(`GetVersion failed: ${response.payload.value.message}`);
    }
    if (response.payload.case !== 'getVersion') {
      throw new Error(`GetVersion: unexpected response case ${String(response.payload.case)}`);
    }
    return response.payload.value;
  }

  private sendRequest(request: ControlRequest): Promise<ControlResponse> {
    const requestId = newRequestId();
    request.requestId = requestId;
    if (!request.traceContext) {
      request.traceContext = new TraceContext({});
    }
    return new Promise<ControlResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new Error(`Control request ${requestId} timed out after ${REQUEST_TIMEOUT_MS}ms`));
      }, REQUEST_TIMEOUT_MS);
      this.pending.set(requestId, { resolve, reject, timer });

      const dispatch = () => {
        if (!this.channel || this.channel.readyState !== 'open') {
          this.queue.push(dispatch);
          return;
        }
        try {
          const payload = request.toBinary();
          for (const chunk of packMessage(nextMessageId(), payload)) {
            this.channel.send(chunk);
          }
        } catch (error) {
          this.rejectPending(requestId, error instanceof Error ? error : new Error(String(error)));
        }
      };

      if (this.channel && this.channel.readyState === 'open') {
        dispatch();
      } else {
        this.queue.push(dispatch);
      }
    });
  }

  private rejectPending(requestId: string, error: Error): void {
    const entry = this.pending.get(requestId);
    if (!entry) return;
    clearTimeout(entry.timer);
    this.pending.delete(requestId);
    entry.reject(error);
  }

  private onChannelChange(sessionId: string, channel: RTCDataChannel | null): void {
    if (channel === null) {
      if (this.channel === null) {
        return;
      }
      logger.warn(`[ControlChannel:${sessionId}] Control channel closed`);
      this.channel = null;
      this.reassembler = new ChunkReassembler();
      const queued = this.queue;
      this.queue = [];
      // Try to fall back to any other open channel before failing pending requests
      const fallback = webrtcService.getAnyOpenControlDataChannel();
      if (fallback) {
        this.attachChannel(fallback);
        for (const fn of queued) fn();
      } else {
        const error = new Error('Control channel closed before response');
        for (const entry of this.pending.values()) {
          clearTimeout(entry.timer);
          entry.reject(error);
        }
        this.pending.clear();
      }
      return;
    }
    this.attachChannel(channel);
  }

  private attachChannel(channel: RTCDataChannel): void {
    if (this.channel === channel) return;
    this.channel = channel;
    this.reassembler = new ChunkReassembler();
    channel.binaryType = 'arraybuffer';
    channel.addEventListener('message', (event: MessageEvent<ArrayBuffer | Blob | string>) => {
      const data = event.data;
      if (typeof data === 'string') return;
      const bufferPromise = data instanceof Blob ? data.arrayBuffer() : Promise.resolve(data);
      void bufferPromise.then((buffer) => {
        const assembled = this.reassembler.pushChunk(buffer);
        if (!assembled) return;
        let response: ControlResponse;
        try {
          response = ControlResponse.fromBinary(assembled);
        } catch (error) {
          logger.error('Failed to decode ControlResponse', {
            'error.message': error instanceof Error ? error.message : String(error),
          });
          return;
        }
        const pending = this.pending.get(response.requestId);
        if (!pending) {
          logger.warn('ControlResponse without matching pending request', {
            'control.request_id': response.requestId,
          });
          return;
        }
        clearTimeout(pending.timer);
        this.pending.delete(response.requestId);
        pending.resolve(response);
      }).catch((error) => {
        logger.error('Failed to process control channel buffer', {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      });
    });
    // Flush queued requests now that the channel is open
    if (channel.readyState === 'open') {
      const queued = this.queue;
      this.queue = [];
      for (const fn of queued) fn();
    }
  }
}

export const controlChannelService = new ControlChannelService();
