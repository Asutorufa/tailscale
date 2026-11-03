// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tsnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"tailscale.com/ipn/store/mem"
	"tailscale.com/net/dnscache"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest"
	"tailscale.com/types/nettype"
	"tailscale.com/util/dnsname"
)

func TestUnderlayDNSHostFallback(t *testing.T) {
	want := netip.MustParseAddr("192.0.2.53")
	dns := startFakeUpstreamDNS(t, map[string][]netip.Addr{"underlay.invalid.": {want}})
	tstest.Replace(t, &net.DefaultResolver, &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("unexpected system DNS fallback")
	}})
	var fallbackCalls atomic.Int32
	forward := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		fallbackCalls.Add(1)
		var d net.Dialer
		return d.DialContext(ctx, network, dns.addr())
	}}
	for _, tc := range []struct {
		name           string
		routed         bool
		missingForward bool
	}{
		{name: "unrouted"},
		{name: "routed", routed: true},
		{name: "missing-forward", missingForward: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &netns.Underlay{DNSCache: &dnscache.Resolver{Forward: forward}}
			if tc.missingForward {
				u.DNSCache.Forward = nil
			}
			s := &Server{Underlay: u}
			res := s.dnsResolver()
			queryErr := errors.New("no DNS upstream available")
			res.queryDNS = func(context.Context, dnsname.FQDN, dnsmessage.Type) ([]byte, bool, error) {
				return nil, tc.routed, queryErr
			}
			res.searchDomains = nil
			before := fallbackCalls.Load()
			got, err := res.lookupIP(t.Context(), "ip4", "underlay.invalid.")
			if tc.routed || tc.missingForward {
				if err == nil {
					t.Fatalf("lookupIP = %v; want an error without system fallback", got)
				}
				if fallbackCalls.Load() != before {
					t.Fatal("used fallback DNS for a configured route or missing resolver")
				}
				return
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("lookupIP = %v, %v; want %v through Underlay", got, err, want)
			}
			if fallbackCalls.Load() == before {
				t.Fatal("lookup bypassed Underlay DNS")
			}
		})
	}
}

type countedUnderlay struct {
	tcp    atomic.Int32
	udp    atomic.Int32
	closed atomic.Int32
}

func (u *countedUnderlay) ListenPacket(ctx context.Context, network, address string) (nettype.PacketConn, error) {
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(ctx, network, address)
	if err != nil {
		return nil, err
	}
	u.udp.Add(1)
	return &countedPacketConn{UDPConn: pc.(*net.UDPConn), u: u}, nil
}

type countedPacketConn struct {
	*net.UDPConn
	u    *countedUnderlay
	once sync.Once
}

func (p *countedPacketConn) Close() error {
	err := p.UDPConn.Close()
	p.once.Do(func() { p.u.closed.Add(1) })
	return err
}
func (u *countedUnderlay) network() *netns.Underlay {
	return &netns.Underlay{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			u.tcp.Add(1)
			var d net.Dialer
			return d.DialContext(ctx, network, address)
		},
		PacketListener: u,
		DNSCache:       &dnscache.Resolver{Forward: net.DefaultResolver},
	}
}

// Uses a local control server, real DERP/STUN sockets and two real WireGuard
// netstacks. No account or privileged network configuration is needed.
func TestIndependentUnderlays(t *testing.T) {
	controlURL, control := startControl(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	var servers [2]*Server
	var underlays [2]countedUnderlay
	for i := range servers {
		servers[i] = &Server{Dir: t.TempDir(), Hostname: fmt.Sprintf("underlay%d", i),
			ControlURL: controlURL, Ephemeral: true, Store: new(mem.Store), Underlay: underlays[i].network()}
		t.Cleanup(func() { servers[i].Close() })
	}
	errs := make(chan error, 2)
	for _, s := range servers {
		go func() { _, err := s.Up(ctx); errs <- err }()
	}
	for range servers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	a, b := servers[0], servers[1]
	waitForPeerReachable(t, a, b.lb.NodeKey())
	waitForPeerReachable(t, b, a.lb.NodeKey())
	for _, s := range servers {
		waitForHomeDERPConnected(t, ctx, s)
	}

	for _, network := range []string{"tcp4", "tcp6", "udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			v4, v6 := b.TailscaleIPs()
			ip := v4
			if strings.HasSuffix(network, "6") {
				ip = v6
			}
			if !ip.IsValid() {
				t.Fatal("testcontrol did not allocate expected address family")
			}
			target := netip.AddrPortFrom(ip, 12345).String()
			var serve func()
			if strings.HasPrefix(network, "tcp") {
				ln, err := b.Listen(network, target)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				serve = func() {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					_, _ = io.Copy(c, c)
				}
			} else {
				pc, err := b.ListenPacket(network, target)
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				serve = func() {
					buf := make([]byte, 128)
					n, from, err := pc.ReadFrom(buf)
					if err == nil {
						_, _ = pc.WriteTo(buf[:n], from)
					}
				}
			}
			go serve()
			// Upstream canonicalization handles case and a trailing root dot.
			c, err := a.Dial(ctx, network, "UNDERLAY1.tail-scale.ts.net.:12345")
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 5)
			if _, err := io.ReadFull(c, buf); err != nil {
				t.Fatal(err)
			}
			if string(buf) != "hello" {
				t.Fatalf("echo %q", buf)
			}
		})
	}
	// Exercise short names and ExtraRecords using the upstream resolution path.
	v4, _ := b.TailscaleIPs()
	control.AddDNSRecords(tailcfg.DNSRecord{Name: "extra.tail-scale.ts.net.", Value: v4.String()})
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, via, err := a.dialer.UserDialPlan(ctx, "tcp", "EXTRA.tail-scale.ts.net.:1")
		if err == nil && via {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ExtraRecords not resolved: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, via, err := a.dialer.UserDialPlan(ctx, "tcp", "UNDERLAY1:1"); err != nil || !via {
		t.Fatalf("short name: via=%v err=%v", via, err)
	}
	for i, s := range servers {
		before := underlays[i].udp.Load()
		s.Sys().MagicSock.Get().Rebind()
		if underlays[i].udp.Load() <= before {
			t.Fatal("Rebind bypassed instance packet listener")
		}
		if underlays[i].tcp.Load() == 0 {
			t.Fatal("control/DERP bypassed instance dialer")
		}
	}
	// Closing one instance must not close the other one's sockets or dialer.
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.dialer.SystemDial(ctx, "tcp", strings.TrimPrefix(controlURL, "http://")); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	for i := range underlays {
		if underlays[i].closed.Load() != underlays[i].udp.Load() {
			t.Fatalf("instance %d leaked sockets: opened=%d closed=%d", i, underlays[i].udp.Load(), underlays[i].closed.Load())
		}
	}
}

func TestUnderlayConfiguration(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Underlay: &netns.Underlay{}}
	if err := s.Start(); err == nil {
		t.Fatal("accepted partial underlay")
	}
	_ = s.Close()
}
