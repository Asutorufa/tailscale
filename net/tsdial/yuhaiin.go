package tsdial

import "net/netip"

func (d *Dialer) ResolveMagicDNS(hostname, network string) (addr netip.Addr, ok bool) {
	if function := d.resolveMagicDNS.Load(); function != nil {
		addr, ok = (*function)(hostname, network)
	}
	return
}
