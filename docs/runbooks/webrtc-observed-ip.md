# WebRTC public IP — observed_ip (contract 5.0.0)

The accelerator's public address is not configured anywhere: the server reports
it back in `RegisterAck.observed_ip` (the control connection's source IP), and
the client injects public ICE candidates from it. A stale hardcoded
`WEBRTC_PUBLIC_IP` (the failure this replaced) can no longer happen.

## Semantics

- **Server**: fills `observed_ip` from the gRPC peer. Omitted only for
  unroutable peers (proxy/LB in front of the control port), logged at error.
- **Client**: missing `observed_ip` on an accepted ack, or a change across
  registrations (home IP rotated), is fatal — `exit(1)` after a critical log;
  the compose `restart: unless-stopped` policy re-registers with the fresh IP.
- **Candidates**: public UDP candidate uses the port the session actually bound
  (range 10000-10199, forwarded on the router); TCP fallback defaults to 60060.
  Optional overrides: `WEBRTC_PUBLIC_PORT` / `WEBRTC_PUBLIC_TCP_PORT`.
- **LAN access**: the container runs with `network_mode: host`, so gathered
  host candidates carry the LAN IP and same-network browsers connect directly.

## Jetson deploy steps

```bash
# 1. Pull the 5.0.0 image (server must already be deployed — CI does it on merge)
./scripts/deployment/jetson-nano/jetson-deploy.sh  # or update ACCELERATOR_IMAGE in .env

# 2. In /opt/josnelihurt/cuda-learning/docker-compose.yml:
#    - add:      network_mode: host
#    - remove:   the ports: block and the WEBRTC_PUBLIC_* env lines

# 3. Recreate
docker compose up -d && docker logs -f cuda-accelerator-client
```

Expected on registration: `Registered, session_id=...` followed by
`Public ICE candidate injected for <ip>:<port>` on the first stream.
