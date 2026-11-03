# Tailscale Lite underlay patch

This branch is for `github.com/yuhaiin/tailscale-lite`, which constructs
`controlclient.Auto` and `wgengine.NewUserspaceEngine` directly. It does not
extend `tsnet.Server`. Existing Yuhaiin users of `Server.Underlay` must stay on
`codex/await-initial-endpoint-update` until their adapter migrates to lite.

The patch supplies per-instance TCP, UDP and DNS through `netns.Underlay`:

- Control HTTP/Noise, DERP and netcheck HTTPS use the injected TCP dialer and
  DNS cache, bypassing ambient proxy selection.
- Magicsock, netcheck probes and external DNS forwarding use the injected UDP
  listener. Host port mapping, raw disco, captive portal checks and underlay
  ICMP probes are disabled for these instances.
- Tailnet DNS forwarding retains lite's `UseNetstackForIP` and host-stack TCP
  and UDP callbacks. External TCP DNS/DoH uses the Underlay even without user
  routes. Injected UDP sockets retain the upstream single-socket group API.
- `AwaitEndpointUpdates` lets lite wait for endpoint discovery before exposing
  its first peer route. Closing magicsock wakes these waiters.

The branch retains upstream `tsnet`, `netcheck.Standalone` and user-name DNS
resolution. Lite resolves application names itself; its DNS forwarder calls
`UserDial` with IP literals, including bootstrapped DoH addresses. This branch
does not promise custom DNS for arbitrary hostname-based `Dialer.UserDial` calls.
Keep that assumption under review when upgrading lite or the upstream resolver.

The base is upstream `f5d378471eb8ae26044fc89ed19e010a1b5a627e` (Go 1.27.3).
The consumer CI pins lite `90aa35361b1f56e29e7c4c7446ea8acd06db96f9`, overrides
its Tailscale dependency with this checkout, and runs race-enabled unit and
integration tests with `ts_omit_netstack,ts_omit_gro`. It also rejects production
dependencies on tsnet, ipnlocal or gVisor.

Local unit and integration suites passed with the race detector. An additional
temporary consumer probe connected two clients using explicit Underlays,
delivered an encrypted IP packet, rebound both clients and verified all their
injected UDP sockets closed on shutdown.

Local validation excludes only `magicsock/TestNetworkSendErrors/network-down`,
whose `sendmmsg: operation not permitted` failure reproduces on unmodified
upstream on this host. Live Tailnet/Headscale interoperability and non-Linux
runtime behavior are not established by these local checks.
