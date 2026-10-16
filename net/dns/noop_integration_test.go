// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package dns

import (
	"errors"
	"net/netip"
	"testing"

	dns "golang.org/x/net/dns/dnsmessage"
	"tailscale.com/control/controlknobs"
	"tailscale.com/health"
	"tailscale.com/net/netmon"
	"tailscale.com/net/tsdial"
	"tailscale.com/util/eventbus/eventbustest"
)

func TestNoopManagerMagicDNS(t *testing.T) {
	oscfg, err := NewNoopManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oscfg.GetBaseConfig(); !errors.Is(err, ErrGetBaseConfigNotSupported) {
		t.Fatalf("GetBaseConfig = %v", err)
	}
	bus := eventbustest.NewBus(t)
	nm := netmon.NewStatic()
	defer nm.Close()
	d := tsdial.NewDialer(nm)
	defer d.Close()
	d.SetBus(bus)
	m := NewManager(t.Logf, oscfg, health.NewTracker(bus), d, nil, &controlknobs.Knobs{}, "linux", bus)
	defer m.Down()
	config := Config{
		Hosts:         hosts("node.test.ts.net.", "100.64.0.2", "fd7a:115c:a1e0::2"),
		Routes:        upstreams("test.ts.net.", ""),
		SearchDomains: fqdns("test.ts.net."),
	}
	if err := m.Set(config); err != nil {
		t.Fatal(err)
	}
	rcfg, ocfg, err := m.compileConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(ocfg.MatchDomains) == 0 {
		t.Fatal("noop manager lost scoped MagicDNS domains")
	}
	if _, ok := rcfg.Routes["."]; ok {
		t.Fatal("no OS resolver: must not install a catch-all forwarding route")
	}
	for _, tt := range []struct {
		name    string
		typ     dns.Type
		want    dns.RCode
		answers int
	}{
		{"NODE.test.ts.net.", dns.TypeA, dns.RCodeSuccess, 1},
		{"node.test.ts.net.", dns.TypeAAAA, dns.RCodeSuccess, 1},
		{"2.0.64.100.in-addr.arpa.", dns.TypePTR, dns.RCodeSuccess, 1},
		{"missing.test.ts.net.", dns.TypeA, dns.RCodeNameError, 0},
		{"node.test.ts.net.", dns.TypeTXT, dns.RCodeSuccess, 0},
	} {
		t.Run(tt.name+tt.typ.String(), func(t *testing.T) {
			q := dns.Message{Header: dns.Header{ID: 0x5678, RecursionDesired: true}, Questions: []dns.Question{{Name: dns.MustNewName(tt.name), Type: tt.typ, Class: dns.ClassINET}}}
			wire, err := q.Pack()
			if err != nil {
				t.Fatal(err)
			}
			wire, err = m.Query(t.Context(), wire, "udp", netip.MustParseAddrPort("127.0.0.1:12345"))
			if err != nil {
				t.Fatal(err)
			}
			var answer dns.Message
			if err := answer.Unpack(wire); err != nil {
				t.Fatal(err)
			}
			if answer.ID != q.ID || answer.RCode != tt.want || len(answer.Answers) != tt.answers {
				t.Fatalf("response: %+v", answer)
			}
		})
	}
}
