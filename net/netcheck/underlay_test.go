// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"

	"tailscale.com/net/dnscache"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest"
)

func TestNodeAddrUsesUnderlayDNS(t *testing.T) {
	tstest.Replace(t, &net.DefaultResolver, &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("unexpected system DNS lookup")
	}})
	node := &tailcfg.DERPNode{HostName: "derp.invalid"}
	v4, v6 := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1")
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint(cache), func(t *testing.T) {
			c := &Client{
				UseDNSCache: cache,
				Logf:        t.Logf,
				Underlay: &netns.Underlay{DNSCache: &dnscache.Resolver{
					SingleHost: node.HostName, SingleHostStaticResult: []netip.Addr{v4, v6},
				}},
				resolver: &dnscache.Resolver{SingleHost: node.HostName, SingleHostStaticResult: []netip.Addr{netip.MustParseAddr("192.0.2.99")}},
			}
			for proto, ip := range map[probeProto]netip.Addr{probeIPv4: v4, probeIPv6: v6} {
				got, ok := c.nodeAddrPort(t.Context(), node, 3478, proto)
				if want := netip.AddrPortFrom(ip, 3478); !ok || got != want {
					t.Fatalf("nodeAddrPort(%v) = %v, %v; want %v through Underlay", proto, got, ok, want)
				}
			}
		})
	}
}
