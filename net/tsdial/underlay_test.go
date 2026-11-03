// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tsdial

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"tailscale.com/net/netmon"
	"tailscale.com/net/netns"
)

func TestSystemDialCloseDuringUnderlayDial(t *testing.T) {
	nm := netmon.NewStatic()
	defer nm.Close()
	d := NewDialer(nm)
	entered, release := make(chan struct{}), make(chan struct{})
	local, remote := net.Pipe()
	defer remote.Close()
	d.Underlay = &netns.Underlay{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		close(entered)
		<-release
		return local, nil
	}}
	done := make(chan error, 1)
	go func() { _, err := d.SystemDial(t.Context(), "tcp", "127.0.0.1:1"); done <- err }()
	<-entered
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := remote.Write([]byte("x")); err == nil {
		t.Fatal("in-flight socket leaked after Close")
	}
}

func TestMagicDNSPlanCanonicalization(t *testing.T) {
	d := &Dialer{UseNetstackForIP: func(netip.Addr) bool { return true }}
	d.SetResolveMagicDNS(func(host, network string) (netip.Addr, bool) {
		if host != "node" && host != "node.tail.ts.net" && host != "extra.tail.ts.net" {
			t.Errorf("uncanonical hostname %q", host)
			return netip.Addr{}, false
		}
		if strings.HasSuffix(network, "6") {
			return netip.MustParseAddr("fd7a:115c:a1e0::1"), true
		}
		return netip.MustParseAddr("100.64.0.1"), true
	})
	for _, name := range []string{"NODE", "NODE.", "NODE.tail.ts.net.", "EXTRA.tail.ts.net."} {
		for _, network := range []string{"tcp4", "tcp6", "udp4", "udp6"} {
			ip, via, err := d.UserDialPlan(t.Context(), network, net.JoinHostPort(name, "80"))
			if err != nil || !via || ip.Addr().Is6() != strings.HasSuffix(network, "6") {
				t.Fatalf("%s %s: %v %v %v", name, network, ip, via, err)
			}
		}
	}
}

func TestUserDialWithoutRoutesHonorsExplicitUnderlay(t *testing.T) {
	for _, target := range []struct {
		network string
		address string
	}{
		{"tcp4", "198.51.100.53:443"},
		{"tcp6", "[2001:db8::53]:443"},
	} {
		t.Run(target.network, func(t *testing.T) {
			nm := netmon.NewStatic()
			defer nm.Close()
			d := NewDialer(nm)
			defer d.Close()
			var called bool
			d.Underlay = &netns.Underlay{
				DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
					called = true
					if network != target.network || address != target.address {
						t.Errorf("Underlay dial = %s %s, want %s %s", network, address, target.network, target.address)
					}
					local, remote := net.Pipe()
					t.Cleanup(func() { _ = remote.Close() })
					return local, nil
				},
			}
			conn, err := d.UserDial(t.Context(), target.network, target.address)
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			if !called {
				t.Fatal("UserDial bypassed explicit Underlay with no routes configured")
			}
		})
	}
}
