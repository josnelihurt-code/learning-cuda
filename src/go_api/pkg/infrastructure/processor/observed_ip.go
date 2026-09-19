package processor

import (
	"context"
	"net"
	"sync"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/peer"
)

// classifyPeerAddr extracts the public IP from a gRPC peer address ("ip:port"),
// or "" when it is not globally routable (private, loopback, malformed).
func classifyPeerAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}

// observedIPFromContext returns the control connection's public source IP, or
// "" when the peer is not routable — a proxy in front of the control port.
func observedIPFromContext(ctx context.Context, log zerolog.Logger) string {
	p, ok := peerFromContext(ctx)
	if !ok || p == nil || p.Addr == nil {
		log.Error().Msg("control stream has no peer address — observed_ip omitted")
		return ""
	}
	ip := classifyPeerAddr(p.Addr.String())
	if ip == "" {
		log.Error().
			Str("peer_addr", p.Addr.String()).
			Msg("control stream peer is not a routable public address (proxy/LB in front of the control port?) — observed_ip omitted")
	}
	return ip
}

// observedIPTracker remembers the last observed public IP per device.
type observedIPTracker struct {
	mu   sync.Mutex
	last map[string]string
}

func newObservedIPTracker() *observedIPTracker {
	return &observedIPTracker{last: make(map[string]string)}
}

// observe records device's current IP and reports whether it changed.
func (t *observedIPTracker) observe(device, ip string) (changed bool, previous string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, seen := t.last[device]
	t.last[device] = ip
	return seen && prev != ip, prev
}

// peerFromContext is a seam for tests.
var peerFromContext = func(ctx context.Context) (*peer.Peer, bool) {
	return peer.FromContext(ctx)
}
