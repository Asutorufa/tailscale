// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package netns

import (
	"tailscale.com/net/dnscache"
	"tailscale.com/net/netx"
	"tailscale.com/types/nettype"
)

// Underlay supplies the physical network used by an embedded tsnet server.
// All fields are required and must be set before Start and remain immutable.
// Implementations must support concurrent calls and honor context cancellation.
// They own namespace/interface binding and loop prevention, replacing netns
// socket controls and environment proxy selection for this server only.
//
// PacketListener must return unconnected UDP sockets with IP local addresses,
// real allocated ports, deadlines, and independent Close/rebind semantics. A
// proxy may supply a virtual local endpoint, but it must be stable for the
// socket lifetime. Returning a *net.UDPConn preserves native socket optimizations.
// Raw disco, ICMP underlay probes, port mapping and captive portal detection are
// disabled: they inspect the host network rather than the supplied transport.
// The supplied transports and DNS cache remain owned by the caller.
type Underlay struct {
	DialContext    netx.DialFunc
	PacketListener nettype.PacketListenerWithNetIP
	DNSCache       *dnscache.Resolver
}
