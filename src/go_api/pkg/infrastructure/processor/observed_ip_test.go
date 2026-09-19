package processor

import (
	"context"
	"net"
	"testing"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/peer"
)

func TestSuccess_ClassifyPeerAddrPublicAddresses(t *testing.T) {
	// Arrange
	cases := []struct {
		name string
		addr string
		want string
	}{
		{"public ipv4", "98.248.157.222:51234", "98.248.157.222"},
		{"public ipv6", "[2607:f8b0:4004:c07::71]:443", "2607:f8b0:4004:c07::71"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := classifyPeerAddr(tc.addr)

			// Assert
			if got != tc.want {
				t.Fatalf("classifyPeerAddr(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}

func TestError_ClassifyPeerAddrNonRoutableAddresses(t *testing.T) {
	// Arrange
	cases := []struct {
		name string
		addr string
	}{
		{"private rfc1918", "192.168.10.213:60062"},
		{"private rfc1918 10/8", "10.0.0.5:60062"},
		{"loopback", "127.0.0.1:60062"},
		{"link local", "169.254.1.2:60062"},
		{"ipv6 unique local", "[fd00::1]:60062"},
		{"not an ip", "vultur.example.com:60062"},
		{"missing port", "98.248.157.222"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := classifyPeerAddr(tc.addr)

			// Assert
			if got != "" {
				t.Fatalf("classifyPeerAddr(%q) = %q, want empty", tc.addr, got)
			}
		})
	}
}

func TestSuccess_ObservedIPFromContext(t *testing.T) {
	// Arrange
	original := peerFromContext
	peerFromContext = func(context.Context) (*peer.Peer, bool) {
		return &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("98.248.157.222"), Port: 51234}}, true
	}
	t.Cleanup(func() { peerFromContext = original })

	// Act
	got := observedIPFromContext(context.Background(), zerolog.Nop())

	// Assert
	if got != "98.248.157.222" {
		t.Fatalf("observedIPFromContext = %q, want 98.248.157.222", got)
	}
}

func TestError_ObservedIPFromContextPrivatePeer(t *testing.T) {
	// Arrange
	original := peerFromContext
	peerFromContext = func(context.Context) (*peer.Peer, bool) {
		return &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.10"), Port: 60062}}, true
	}
	t.Cleanup(func() { peerFromContext = original })

	// Act
	got := observedIPFromContext(context.Background(), zerolog.Nop())

	// Assert
	if got != "" {
		t.Fatalf("observedIPFromContext = %q, want empty for private peer", got)
	}
}

func TestError_ObservedIPFromContextNoPeer(t *testing.T) {
	// Arrange
	original := peerFromContext
	peerFromContext = func(context.Context) (*peer.Peer, bool) { return nil, false }
	t.Cleanup(func() { peerFromContext = original })

	// Act
	got := observedIPFromContext(context.Background(), zerolog.Nop())

	// Assert
	if got != "" {
		t.Fatalf("observedIPFromContext = %q, want empty without peer", got)
	}
}

func TestSuccess_ObservedIPTrackerDetectsChange(t *testing.T) {
	// Arrange
	sut := newObservedIPTracker()

	// Act
	firstChanged, firstPrev := sut.observe("jetson-prod-01", "73.71.7.90")
	sameChanged, _ := sut.observe("jetson-prod-01", "73.71.7.90")
	changed, previous := sut.observe("jetson-prod-01", "98.248.157.222")

	// Assert
	if firstChanged {
		t.Fatal("first observation must not report a change")
	}
	if firstPrev != "" {
		t.Fatalf("first observation previous = %q, want empty", firstPrev)
	}
	if sameChanged {
		t.Fatal("repeated IP must not report a change")
	}
	if !changed {
		t.Fatal("new IP must report a change")
	}
	if previous != "73.71.7.90" {
		t.Fatalf("change previous = %q, want 73.71.7.90", previous)
	}
}
