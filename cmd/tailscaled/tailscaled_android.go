package main

import (
	"github.com/wlynxg/anet"
	"tailscale.com/net/netmon"
)

func ini() {
	netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
		ifs, err := anet.Interfaces()
		if err != nil {
			return nil, err
		}
		ret := make([]netmon.Interface, len(ifs))
		for i := range ifs {
			addrs, err := ifs[i].Addrs()
			if err != nil {
				return nil, err
			}
			ret[i] = netmon.Interface{
				Interface: &ifs[i],
				AltAddrs:  addrs,
			}
		}

		return ret, nil
	})
}
