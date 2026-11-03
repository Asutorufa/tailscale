// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package magicsock

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestAwaitEndpointUpdates(t *testing.T) {
	for _, outcome := range []string{"complete", "cancel", "close"} {
		t.Run(outcome, func(t *testing.T) {
			c := &Conn{endpointsUpdateActive: true}
			c.muCond = sync.NewCond(&c.mu)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- c.AwaitEndpointUpdates(ctx) }()
			select {
			case err := <-result:
				t.Fatalf("returned before update completed: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
			switch outcome {
			case "complete":
				c.mu.Lock()
				c.endpointsUpdateActive = false
				c.muCond.Broadcast()
				c.mu.Unlock()
			case "cancel":
				cancel()
			case "close":
				c.mu.Lock()
				c.closed = true
				c.muCond.Broadcast()
				c.mu.Unlock()
			}
			var want error
			if outcome == "cancel" {
				want = context.Canceled
			}
			if outcome == "close" {
				want = net.ErrClosed
			}
			if err := <-result; !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}
