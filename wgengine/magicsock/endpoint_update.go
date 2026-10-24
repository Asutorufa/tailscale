// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package magicsock

import (
	"context"
	"net"
)

// AwaitEndpointUpdates waits for active and queued endpoint discovery to finish.
// During initial discovery, netcheck may rebind UDP and reconnect DERP after
// publishing NetInfo. Sending the first handshake in that interval can lose it
// and incur a full WireGuard retry. This waits for completion without a delay
// heuristic; future network changes can still trigger another discovery.
func (c *Conn) AwaitEndpointUpdates(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		c.mu.Lock()
		c.muCond.Broadcast()
		c.mu.Unlock()
	})
	defer stop()
	for c.endpointsUpdateActive && !c.closed && ctx.Err() == nil {
		c.muCond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed {
		return net.ErrClosed
	}
	return nil
}
