# Starfarer

Godot C# client and Go authoritative server, with Protobuf over UDP. Each project runs independently. There are no PowerShell script files or launcher dependencies between them.

## Structure

- `starfarer/netcode/`: generic C# timing, prediction history, interpolation, reliable delivery, fragment assembly, traffic budgets, and packet protection. Compiled into the Godot project.
- `starfarer/netcode/godot/`: Godot datagram adapter.
- `starfarer/Networking/`: game protocol, session integration, and the movement model.
- `starfarer/Scenes/`: presentation of predicted local and interpolated remote players.
- `starfarer_server/netcode/`: generic Go transport, timelines, reliability, spatial indexing, scheduling, budgets, and packet protection.
- `starfarer_server/server.go`: game sessions, event dispatch, AOI selection, and snapshot composition.
- `starfarer_server/internal/world/`: authoritative movement rules.
- `starfarer_server/proto/`: shared game contract.

See [architecture and guarantees](starfarer_server/netcode/ARCHITECTURE.md), [server netcode](starfarer_server/netcode/README.md), and [client netcode](starfarer/netcode/README.md).

## Generate and build

From `starfarer_server`:

```text
buf lint
buf generate
go test ./...
go build ./...
```

Buf generates Go and C# from the same contract. Generated files are ignored by Git; regenerate after checkout and contract changes. Remote Buf plugins require network access.

From `starfarer`:

```text
dotnet build starfarer.csproj
```

Godot .NET and .NET 8 are required. NuGet.Config currently references the locally installed Godot package directory; adjust that source for another installation.

**Envelope protocol is now version 2. Restart older server binaries and rebuild the client together.** The Protobuf namespace remains `starfarer.protocol.v1`.

## Run separately

From `starfarer_server`:

```text
go run .
```

Open `starfarer/project.godot` in Godot .NET. Open `Scenes/World.tscn` and press F6, or F5 for the diagnostic scene. WASD drives the predicted local capsule; Go remains authoritative. Open a second world instance to see remote interpolation. The diagnostic button sends a confirmed echo action; its result appears once in the log even after retries.

The default address is `127.0.0.1:7777`. The client supports `-- --server-host=HOST --server-port=PORT` when started from the command line. Sessions resume across transient connection loss while retained by the server.

## Live rates and visibility

```text
go run . -simulation-hz 30 -snapshot-hz 10 -aoi-radius 100
```

`simulation-hz` is the server callback frequency. Movement uses a fixed 60 Hz input timeline, consumed in batches by those callbacks. Changing callback frequency never changes movement speed or asks the client to rebuild. It does not eliminate the CPU cost of authoritative simulation.

For live settings:

```text
go run . -settings netcode/settings.example.json
```

Edit `simulation_hz` and `snapshot_hz` in that JSON while running. Valid changes are loaded within about one second and announced in snapshots. No reconnect is required. Snapshot rate is also reduced per client when complete snapshot acknowledgements stop or its outbound budget is exhausted, then recovers gradually.

AOI uses a spatial grid, a configurable entry radius, and a 20% larger exit radius. Visibility is sent as complete, bounded, fragmented baselines. A missing fragment does not remove players. Leaving visibility does not destroy server state.

## Optional private-deployment protection

Set `STARFARER_NET_KEY` to the same base64-encoded, cryptographically random 32-byte secret in the Go process and Godot/.NET environment before starting them. Restart Godot if it was launched before the environment variable was set. Never commit the key or put it in command-line arguments.

This enables AES-256-GCM packet protection, authenticated headers, and replay rejection. Hosts need UTC clocks within 30 seconds. Unprotected mode binds to loopback only. The shared key authenticates a trusted group, not individual accounts: every holder can impersonate another holder. This is not a public-game login system or a forward-secret key exchange.

## Tests

Server:

```text
go test ./...
go vet ./...
```

Client:

```text
dotnet run --project netcode/tests/Starfarer.Netcode.Tests.csproj
```

Optional interoperability checks connect to a server you start yourself; they do not launch or stop it:

```text
dotnet run --project netcode/tests/Starfarer.Netcode.Tests.csproj -- --integration 7777
dotnet run --project netcode/tests/Starfarer.Netcode.Tests.csproj -- --integration 7777 --impaired
```

The impaired check uses seeded 10% packet loss, approximately 150 ms RTT, up to 60 ms directional jitter, duplication/reordering, and a five-second outage. It checks resumed identity, bounded recovery, prediction, and a deduplicated action result.

## Scope

This implementation supports the current planar movement controller on one server. Ship physics, collision-compatible Go/C# controllers, combat hit validation/rewind, account authentication, and durable world/action storage belong to subsequent game/backend integration. Server meshing is explicitly deferred.

The server currently admits 128 retained sessions and at most 128 visible entities per client. These are resource limits, not measured production capacity. See the architecture document for exact bounds and failure behavior.
