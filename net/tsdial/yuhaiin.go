package tsdial

func (d *Dialer) GetDNSMap() dnsMap {
	d.mu.Lock()
	dns := d.dns
	d.mu.Unlock()

	return dns
}
