// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package magicsock

import (
	"net"
	"slices"
	"testing"

	"tailscale.com/net/netns"
	"tailscale.com/tstest"
	"tailscale.com/types/nettype"
)

func TestUDPSockets(t *testing.T) {
	if !hasUDPTransport {
		t.Skip("no UDP transport")
	}
	const sockets = 4
	wantSharedPort := func(t *testing.T, ruc *RebindingUDPConn) {
		t.Helper()

		if got := ruc.group.bound; got != sockets {
			t.Fatalf("bound %d sockets, want %d", got, sockets)
		}
		for i, pconn := range ruc.group.conns[:ruc.group.bound] {
			if got := uint16(pconn.LocalAddr().(*net.UDPAddr).Port); got != ruc.Port() {
				t.Errorf("socket %d bound to port %d, want %d", i, got, ruc.Port())
			}
		}
	}
	c, _ := newTestConnAndRegistry(t, func(o *Options) { o.UDPSockets = sockets })

	wantSharedPort(t, &c.pconn4)
	c.Rebind()
	wantSharedPort(t, &c.pconn4)
}

func TestUDPSocketsOne(t *testing.T) {
	if !hasUDPTransport {
		t.Skip("no UDP transport")
	}
	for _, tt := range []struct {
		name     string
		sockets  int
		listener nettype.PacketListener
		underlay *netns.Underlay
		noReuse  bool // clear canShareUDPPort
	}{
		{name: "unset", sockets: 0},
		{name: "negative", sockets: -3},
		{name: "test-only-listener", sockets: 4, listener: localhostListener{}},
		{name: "underlay", sockets: 4, underlay: &netns.Underlay{PacketListener: nettype.MakePacketListenerWithNetIP(localhostListener{})}},
		{name: "no-reuseport", sockets: 4, noReuse: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noReuse {
				tstest.Replace(t, &canShareUDPPort, false)
			}
			c, _ := newTestConnAndRegistry(t, func(o *Options) {
				o.UDPSockets = tt.sockets
				o.TestOnlyPacketListener = tt.listener
				o.Underlay = tt.underlay
			})

			if got := c.pconn4.group.bound; got != 1 {
				t.Errorf("bound %d sockets, want 1", got)
			}
			boundPort := c.pconn4.currentConn(0).LocalAddr().(*net.UDPAddr).Port
			if boundPort == 0 || c.pconn4.Port() != uint16(boundPort) {
				t.Fatalf("reported port %d differs from allocated port %d", c.pconn4.Port(), boundPort)
			}
			if _, _, err := c.bind.Open(0); err != nil {
				t.Fatal(err)
			}
			want := []string{"ReceiveIPv4", "ReceiveIPv6", "ReceiveDERP"}
			if got := c.bind.ReceiveNames(); !slices.Equal(got, want) {
				t.Errorf("ReceiveNames = %q, want %q", got, want)
			}
		})
	}
}
