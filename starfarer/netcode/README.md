# Client netcode

Sources in `starfarer/netcode` compile directly into the Godot client. There is no separate runtime library project. The namespace is `Starfarer.Netcode`; core classes do not reference Godot, game types, or Protobuf.

## Active components

- `Transport.cs`: bounded datagram channel and injected codec/transport interfaces.
- `PredictionTimeline.cs`: bounded tick-addressed history, authoritative reconciliation, and pure replay through a game-supplied function.
- `NetworkClock.cs`: echoed-clock estimation, RTT/jitter estimation, prediction lead, and interpolation delay.
- `SnapshotBuffer.cs`: generic timestamped interpolation, holding the most recent sample when future data is absent.
- `BaselineAssembler.cs`: atomic assembly of complete visibility sets, expiration, and memory bounds.
- `ReliableChannel.cs`: bounded, ordered, stop-and-wait actions, retry scheduling, application acceptance acknowledgements, and deduplication.
- `TrafficBudget.cs`: outbound byte token bucket.
- `ProtectedTransport.cs`: optional PSK AES-GCM protection, compatible with Go's packet format.
- `godot/GodotDatagramTransport.cs`: engine socket adapter.

`PendingInputs.cs` and `FixedStepClock.cs` remain independently tested utility primitives. The live movement path uses `PredictionTimeline`; it does not wait for contiguous acknowledgement of every generated input.

## Game integration

`../Networking/GameClient.cs` owns the Protobuf/session adapter, complete-baseline handling, reconnect, and presentation correction. `MovementPrediction.cs` supplies the same planar movement function as Go. `GameEventCodec.cs` validates the versioned envelope.

The local capsule uses predicted state every rendered frame, with fractional presentation and decaying visual correction. Authoritative state is restored and unresolved future inputs replayed after complete snapshots. Remote players interpolate in server time; delay accounts for RTT, arrival jitter, and the advertised snapshot cadence.

Godot passes monotonic elapsed time to the client, independent of game time scaling. Missing confirmations no longer create a permanent strict-sequence backlog. Long interruptions can still require visible corrections; prediction cannot know unseen authoritative events.

## Sessions and actions

Reconnect reuses the resume token and increments the connection attempt. Welcome must echo the connection nonce; subsequent messages must match the accepted epoch. An old connection cannot resume control. The server retains identity/state for two minutes in memory; restart or expiration creates a new session, reports that transition, and discards old pending actions.

The confirmed diagnostic action is an echo. New game actions should define typed Protobuf payloads in the game layer, validate them authoritatively, and use the reliable byte channel. Do not execute irreversible effects while replaying movement. Durable exactly-once effects require an application transaction/idempotency ledger.

## Verification

From the client directory:

```text
dotnet run --project netcode/tests/Starfarer.Netcode.Tests.csproj
dotnet build starfarer.csproj
```

The separate project under `tests/` is only a headless test runner. It links the same sources and is not a runtime dependency. Optional integration mode connects to an already-running server. `.gdignore` prevents asset import without preventing C# compilation.

See `../../starfarer_server/netcode/ARCHITECTURE.md` for protocol limits, security assumptions, and deferred scope.
