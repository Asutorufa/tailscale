# Embedded underlay transport

`Server.Underlay` is the only configuration an embedder needs to supply. Leave it
nil for upstream network-namespace controls, socket options and environment proxy
behavior. Set it before `Start` and do not mutate it afterwards.

An underlay supplies a concurrent context-aware TCP dial function, an unconnected
UDP `nettype.PacketListenerWithNetIP`, and a private `dnscache.Resolver`. Socket
creation failures are propagated; they never fall back to the host network.
Custom transports own interface binding and prevention of routing back into the
embedding proxy. Returning `*net.UDPConn` preserves magicsock's native batching,
GSO/GRO and socket-buffer handling. Virtual connections must report a UDP local
endpoint and allocated port, implement both netip packet methods, deadlines and
independent close/rebind behavior. Proxy transports may have different NAT and
port-binding capabilities; this interface cannot make a connected UDP stream an
unconnected socket.

The configuration is carried through tsdial system connections, control HTTP and
Noise, DERP region connections, magicsock, netcheck, external UDP DNS forwarding,
and log upload. Control and netcheck use the private DNS cache. Underlay user-name
resolution uses its configured resolver unless exit-node DNS applies. The
supplied transports and DNS cache belong to the embedder, not to `Server.Close`.
The server closes the connections it creates. A dial completing concurrently
with tsdial shutdown is closed instead of being registered after shutdown.

When an underlay is supplied, host-specific port mapping, raw disco, captive
portal detection and underlay ICMP probes are disabled for that instance. Their
host-network sockets cannot describe a remote proxy's network. STUN, disco over
UDP, DERP and peer-relay datagrams still use the supplied transports. Tailnet ICMP
through WireGuard/netstack is unaffected. Explicit underlays bypass HTTP proxy
environment selection; nil-underlay servers keep upstream behavior, including
`ALL_PROXY`, `IsSOCKSDialer`, `SetEnabled(false)` and platform socket controls.

`Server.Dial` retains upstream behavior: destinations without a Tailscale route
can use the host network. Embedders implementing a tailnet-only outbound must
check `UserDialPlan` and pin its returned IP to netstack. Calling `Server.Dial`
after a separate route check leaves a route-change window for system fallback.

## Historical patches

| Commit | Disposition |
| --- | --- |
| `2d639291` | Replaced global dialer/listener wrapping with explicit instance configuration. `netns.Listener` again returns `*net.ListenConfig`; netns implementation matches upstream. |
| `81a7de24` | Removed; no internal DNS-map export is needed. |
| `4b5e5f66` | Removed; `noopManager.GetBaseConfig` again returns `ErrGetBaseConfigNotSupported`. The manager already handles it. |
| `f377f7ca` | Removed; `UserDialPlan` already canonicalizes MagicDNS names and supports address-family selection and ExtraRecords. |

The fork is based on upstream `e9d5cdc3231edd3f93254c16f505fd86344a9faf`
(as fetched on 2026-10-16). The single integration commit was rebased from
`9128778b6` across 56 upstream commits without conflicts or changes to its Go
patch. No DNS-manager workaround or response synthesis is needed. Future
rebases need only preserve underlay plumbing, the in-flight
system-dial shutdown fix (a candidate for upstreaming), and regression tests.
Neither `netns`' default API/implementation nor MagicDNS resolution needs a fork
patch.

## Validation

Initial implementation validation on Linux with Go 1.27.2, before the rebase
from upstream `9128778b6`:

- `go test ./net/netns ./net/tsdial ./net/dns ./net/udprelay ./tsnet ./wgengine/magicsock`
  passes except `magicsock/TestNetworkSendErrors/network-down`: the host rejects
  `sendmmsg` with `operation not permitted`. The same failure was reproduced on
  unmodified upstream `9128778b6`.
- `GOMAXPROCS=4 go test -p=4 -timeout=5m ./...` was executed. In addition to that
  magicsock failure, `cmd/containerboot/TestContainerBoot/kube_shutdown_during_state_write`
  cannot bind host port 9002, and `tstest/integration/TestC2NDebugNetmap` panics with
  `body closed by handler`. Both were reproduced on `9128778b6`.
- Race tests covered netns, tsdial, DNS, DNS resolver, netcheck, UDP relay, tsnet,
  magicsock, DERP HTTP, controlclient, control HTTP and Noise. Only the same
  magicsock assertion failed; no race detector report was emitted.
- `TestIndependentUnderlays` runs two concurrent servers against local
  control/DERP/STUN services with real sockets and WireGuard/netstack. It checks
  TCP/UDP over IPv4/IPv6, case/trailing-dot/short MagicDNS names, ExtraRecords,
  rebinding, separate transport use, socket closure and instance isolation.
- `TestNoopManagerMagicDNS` checks native A/AAAA/PTR/NXDOMAIN/NODATA and absence of
  a fabricated default resolver. Existing empty-base-config, split-DNS,
  IPv6-only and dual-stack tests remain in place.

Production Tailnet credentials, internet NAT traversal, real peer relays, remote
proxy UDP mappings and platform runtime behavior require separate validation.
Local tests do not establish production throughput or remote NAT compatibility.

Library compilation of the changed packages passed with `CGO_ENABLED=0` for
Linux/amd64, macOS/arm64, Windows/amd64, Android/arm64 and iOS/arm64. The mobile
checks do not link an application or exercise platform interface binding.

## Rebase validation (2026-10-16)

The upstream base is `e9d5cdc3231edd3f93254c16f505fd86344a9faf`. Rebase applied
without conflicts; `git range-diff` reported the Go patch unchanged. The old
netns and noop DNS implementations still have no private diff. Upstream raised
the minimum Go version to 1.27.2 and updated WireGuard and DNS forwarding; the
consumer must use that minimum version when updating its dependency pin.

- The focused 12-package run (netns, tsdial, DNS, resolver, netcheck, UDP relay,
  tsnet, magicsock, DERP HTTP, controlclient, control HTTP and Noise) passed except
  the existing magicsock network-down `sendmmsg EPERM` assertion. It reproduces
  on unmodified `e9d5cdc32`.
- The affected-package race run passed tsnet (including independent local
  control/DERP/STUN-backed nodes), tsdial, netns, DNS, netcheck, DERP and control
  packages. Resolver `TestForwarderRaceLoserNoReadError` times out waiting for
  losing UDP reads on both this branch and unmodified `e9d5cdc32`. The exhaustive
  UDP relay VNI allocation test exceeded the four-minute race-test budget; it
  enumerates the entire VNI range and is unchanged by this fork. A separate
  `go test -race ./net/udprelay -run '^TestServer$' -count=1 -timeout=90s` passed
  the real UDP socket cases. No race detector report was emitted.
- `GOMAXPROCS=4 go test -p=4 -timeout=5m ./...` was run. Containerboot's occupied
  port 9002 and compositedav's TTL-cache goroutine-count failures reproduce on
  unmodified `e9d5cdc32`; the magicsock host restriction is described above.
- Changed library packages built with `CGO_ENABLED=0` for Linux/amd64,
  macOS/arm64, Windows/amd64, Android/arm64 and iOS/arm64. As before, these are
  library build checks, not mobile application linkage or platform runtime tests.
- The yuhaiin adapter's local control-server integration suite passed against
  this rebased tree using a temporary module file, before publishing a new pin.

## Rebase validation (2026-11-03)

Rebased the five integration commits from `e9d5cdc32` onto upstream
`f5d378471eb8ae26044fc89ed19e010a1b5a627e`, incorporating 112 upstream
commits. The minimum Go version is now 1.27.3. Conflicts were limited to:

- `net/tsdial`: retain the new Tailnet-policy resolver callback, using the
  Underlay resolver only on the existing fallback path.
- `wgengine/magicsock`: adapt the injected single UDP socket to the new socket
  group API. Custom Underlays keep one socket per address family, ignore
  `Options.UDPSockets`, report the allocated port, and retain socket options.
  Native multi-socket behavior is preserved for instances without an Underlay.

Upstream also added a host-DNS fallback inside tsnet's policy resolver. It now
uses the instance's `Underlay.DNSCache.Forward` when configured; a missing
forward resolver returns an error. Configured Tailnet DNS routes still never
fall back after a resolution error. New regressions cover these DNS paths and
the injected UDP socket's group size, port, and receive handlers.

- The affected 11-package suite passed after excluding only
  `magicsock/TestNetworkSendErrors/network-down`. That test fails with
  `sendmmsg: operation not permitted` on both this branch and unmodified
  upstream `f5d378471` on this host.
- Focused race tests passed twice for tsdial Underlay behavior, tsnet instance
  isolation and LookupIP policy, magicsock UDP socket groups, and endpoint
  synchronization. No race detector reports were emitted.
- Yuhaiin's `go test ./pkg/net/proxy/tailscale` passed using a temporary module
  file pointing at this checkout; its tracked dependency pins were not changed.
- `gofmt` and `git diff --check` passed. The Underlay CI workflow now covers
  the new DNS-policy and UDP socket-group regressions.

Live Tailnet/Headscale interoperability and non-Linux platform runtime behavior
were not revalidated in this rebase.
