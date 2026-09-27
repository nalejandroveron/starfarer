# Server netcode

Game-independent package `starfarer/netcode`, within the existing Go module. There is no additional module or client launcher.

- `udp_host.go`: bounded UDP datagrams, serialized callbacks, cancellation, runtime reconfiguration, and optional per-peer packet protection.
- `scheduler.go`: independent callback/snapshot cadence with bounded scheduling catch-up.
- `timeline.go`: authoritative input deadlines, resolved vs applied frontiers, bounded fallback and recovery.
- `reliable.go`: bounded ordered actions with retry and deduplication.
- `interest.go`: spatial grid, distance selection, hysteresis, and visibility capacity.
- `budget.go`: token buckets.
- `protection.go`: PSK authenticated encryption and replay windows.
- `config.go`: validated rate/datagram limits.

The core never imports the world, Protobuf, or game-specific operations. `input_buffer.go` remains a strict-order utility; movement now uses `Timeline`.

## Integration

`main.go` composes the host, optional live JSON settings, environment key, and game server. `server.go` manages resumable sessions, typed event routing, AOI, complete baseline fragments, and per-client snapshot rates. `internal/world/player.go` validates direction and supplies movement to the generic timeline.

```text
go run .
go run . -simulation-hz 20 -snapshot-hz 7 -aoi-radius 100
go run . -settings netcode/settings.example.json
```

Each command is an alternative startup configuration. The last watches the supplied JSON for live callback/snapshot rate changes. The canonical movement input quantum remains 1/60 second, so existing commands do not change meaning during reconfiguration. Lowering callback frequency does not reduce the number of logical movement steps required by elapsed time.

A missing movement command briefly retains direction for at most six extra ticks, then becomes neutral. The authoritative frontier always advances. Late commands are rejected. More packets cannot purchase more simulation time. A pause longer than two seconds skips the oldest simulation interval, clears stale held input, and bounds catch-up to 120 ticks.

## Verify independently

```text
go test ./...
go vet ./...
```

Tests cover the input timeline, equivalent movement at multiple callback rates, resumption/stale epochs, reliable delivery, AOI hysteresis, 65-entity fragment sizing, authenticated encryption, scheduler changes, and a seeded matrix of 96 latency/jitter/loss/outage combinations.

For actual Go/C# interoperability, start Go yourself and run the optional .NET test mode described in the root README. No project starts the other.

## Deployment boundary

Unencrypted mode is limited to loopback. For a trusted private deployment, both processes may use a base64-encoded 32-byte `STARFARER_NET_KEY`. The PSK proves group membership, not account identity, and provides no forward secrecy. UTC clocks must differ by less than 30 seconds. See `ARCHITECTURE.md` for wire details and limitations.

Session/action state is in memory. No database persistence, combat controller, or meshing runtime is supplied by this package.
