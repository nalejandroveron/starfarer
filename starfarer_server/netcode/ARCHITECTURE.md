# Starfarer networking architecture

Status: implemented single-server networking for the current planar controller. Server meshing is explicitly deferred by the user. This document records implemented behavior, resource bounds, and the remaining game/deployment work; it is not a production scale certification.

## Dependency boundary

Client: Godot presentation -> game adapter (`Networking`) -> generic netcode -> datagram transport.
Server: host composition + game adapters/world -> generic Go netcode.

Core sources live in each project's `netcode` folder. C# compiles into the existing Godot assembly; Go uses the existing module. Only the headless test runner has its own C# project. No launch scripts or runtime project references join the applications.

Game Protobuf contracts remain under `proto/starfarer/protocol/v1`. The envelope is version 2 and rejects version 1. Codecs, entity representations, movement rules, action payloads, and authorization belong to the game adapter. Tests may import those adapters.

## Time and movement

The authoritative input timeline has a 1/60-second quantum. An input's sequence is its target tick on that timeline, not a request for extra simulation time. Server time is monotonic time since this server process started.

The server's configurable callback frequency determines when elapsed logical ticks are consumed. For example, 20 callbacks per second normally consume three 60 Hz movement steps per callback. Snapshot frequency is independent. A live callback-rate change does not reinterpret historical inputs or require a new client binary. This choice preserves the controller's step semantics; it does not make expensive physics cheaper when callbacks are less frequent.

The server accepts finite, normalized movement direction at ticks strictly after the resolved frontier and at most 256 ticks ahead. Duplicate ticks cannot overwrite commands. It resolves every elapsed tick:

1. Use the input explicitly assigned to that tick, when present.
2. Otherwise retain the last applied direction for at most six additional ticks.
3. Then simulate neutral input until a new valid command arrives.

`resolved_tick` includes fallback steps. `last_processed_input` identifies the last actual command used; it must not be treated as the resolved frontier. Late input cannot rewrite authoritative history. Arbitrarily fast packet sending never grants extra simulation time.

The client estimates server time using echoed probes, generates future inputs with a lead derived from RTT and jitter, and sends recent batches of at most 16 commands. Recent redundancy recovers losses without blocking newer movement behind an old hole. Server and client histories are bounded at 256 entries.

A callback catches up at most 120 movement ticks after a stall. Older time is explicitly skipped and held input cleared. The scheduler also reports dropped scheduling steps. This keeps recovery bounded; a severe server pause can lose simulation time and requires an authoritative correction. There is no claim of uninterrupted simulation through an arbitrary process stall.

## Prediction, reconciliation, and presentation

`PredictionTimeline<TInput,TState>` retains future commands and replays a game-supplied pure simulation function. On a complete authoritative baseline, the client removes history at or before `resolved_tick`, restores state, and replays the remaining inputs. A frontier ahead of generated history is valid after fallback or a client pause.

The current Go/C# movement functions share speed, direction normalization, and quantum. Matching vector tests and actual UDP tests exercise them. The predicted local capsule updates every render frame, with fractional presentation between logical steps. Small authoritative corrections decay visually; large corrections snap. No persistence, rewards, sounds, or other irreversible effects belong inside replay.

Remote state is buffered using server timestamps. Render delay includes estimated one-way delay, two snapshot intervals, and jitter margin. When future samples are unavailable, the renderer holds the last state rather than extrapolating indefinitely. Clock offset changes are slewed to avoid abrupt timeline shifts.

Current state is X/Z position only. Collision, grounded state, velocity, ship orientation, moving coordinate frames, hit validation, and combat rewind must be implemented with their corresponding game controllers. Jolt and an unrelated Go physics engine are not assumed equivalent. Generic prediction cannot supply missing game physics.

## Live configuration and congestion response

An optional JSON settings file is checked once per second. Valid `simulation_hz` and `snapshot_hz` values (1..240) update the running host. Snapshots carry timing revision and both rates. Since the logical input quantum stays fixed, no input-history migration is necessary. The effective unique state rate cannot exceed the logical timeline or available callback throughput.

Per-client snapshot rate drops when complete baseline acknowledgements stop advancing for a second, or its outbound byte budget cannot fit the next complete baseline. With fresh acknowledgements it increases gradually toward the configured maximum. Obsolete state is never queued for later delivery.

The current byte budget is 32 KiB/s per client, with a 16 KiB server burst and 4 KiB client burst. Server ingress is bounded at 240 decoded events/s per session (480 burst) and 8,000 events/s globally (16,000 burst). Datagram size bounds apply before Protobuf decoding; cryptographic verification precedes game deserialization in protected mode. These application controls do not replace infrastructure-level flood protection or a mature Internet congestion-control protocol.

## AOI and coherent baselines

A spatial grid selects nearby entities. Entry radius is configurable; exit radius is 20% larger to reduce visibility churn. Self is always included. When capacity is reached, nearest entities take precedence, with stable ID tie-breaking.

Each snapshot describes a complete selected visibility set. It is split into up to seven current-format parts of at most 20 entities, each below the 1100-byte plaintext datagram limit. The client accepts at most eight parts, eight concurrent incomplete baselines, and 128 entities in a completed baseline. Incomplete baselines expire after two seconds.

A set is applied only when every part is present and consistent. Newer complete sets supersede older ones. A missing fragment never means an entity disappeared. Repeated full sets repair lost entry/exit information, and leaving AOI removes only a client visual, never authoritative state. This avoids requiring a separate guaranteed lifecycle stream for the current baseline protocol.

There are no delta baselines, compression, large-entity/sensor policies, or streamed terrain payloads yet. Dense regions cost more bandwidth and may reduce update rate. Large content should use a separate content-transfer path rather than this small realtime packet channel.

## Reliable actions

The generic byte channel has an independent ordered stop-and-wait queue, capped at 64 outgoing actions of at most 512 bytes each. Retries do not block movement or snapshots. Sequence acknowledgements mean the receiving application accepted the payload. Duplicate delivery is acknowledged without executing its callback again. Invalid/future acknowledgements cannot remove queued work.

The client adapts retry delay to measured RTT within 100 ms..1 s; the server currently retries at 250 ms. Snapshot and action traffic share the bounded outgoing budget. Backpressure is explicit when an action queue fills.

The integrated game payload is a typed diagnostic echo, available from the HUD. Production construction/inventory/combat actions should define their own Protobuf payloads, validation, correlation IDs, and effects. The channel retains deduplication only for the retained in-memory session. Durable effects require a database transaction/idempotency ledger; delivery acknowledgement alone cannot guarantee exactly-once persistence through a process crash.

## Sessions and recovery

Initial Hello carries a random 16-byte connection nonce and monotonically increasing attempt number. Welcome echoes the nonce and timing probe, and supplies a random 32-byte resume token, connection epoch, player state, and reliable-action frontier. The initial epoch is random so packets from an older server process do not match a newly created session merely because player IDs were reused.

Transient reconnect presents the token with a new nonce and higher attempt. The server preserves player state and action queues, advances the epoch, replaces the endpoint, and clears queued future movement from the previous connection. Stale epochs, old attempts, and mismatched endpoints cannot control the resumed session. Retried handshakes do not reset history.

A client stops receiving after five seconds before attempting automatic recovery with bounded exponential backoff. The server hides inactive entities after five seconds but retains their session for two minutes. Restart or expiry loses that in-memory state; a fresh session is explicitly detected, and pending old actions are not silently replayed into it. This is not a persisted player identity system.

## Packet protection

Loopback may use plaintext UDP. Binding elsewhere requires a 32-byte PSK supplied through `STARFARER_NET_KEY`, base64 encoded. Never commit or log it. The client uses the same environment key through an optional transport decorator.

Protected packets use standard-library AES-256-GCM. Format: `SFN2` magic (4 bytes), random sender ID (16), big-endian sequence (8), authenticated Unix-millisecond timestamp (8), ciphertext, 16-byte tag. HMAC-SHA256(PSK, sender ID) derives the sender-specific key. The GCM nonce is four zero bytes followed by the 64-bit sequence. Header bytes are authenticated as associated data. A sender stops before counter reuse.

The server allocates a separate outbound sender identity per destination, so one client's traffic cannot consume another client's replay window. A 64-packet bitmap permits bounded reordering and rejects duplicates. A +/-30-second timestamp window prevents old authenticated packets becoming valid after replay-state expiration. Sender tables are bounded and old Go entries expire after two minutes. Hosts therefore need UTC clocks within 30 seconds, independently of the monotonic gameplay clock.

The protected packet limit is 1152 bytes (1100 plaintext plus 52 overhead). Invalid authentication is dropped without per-packet logs. Tests cover wrong keys, mutation, duplicates, reordered valid traffic, expiration, and Go/.NET/Godot interoperability.

**Security boundary:** a shared PSK proves trusted-group membership, not individual account identity. Any key holder can impersonate another holder and derive other senders' keys. There is no forward secrecy, public login, per-account authorization, or external security audit. Do not describe this private-deployment mode as a completed public-game security system. A public deployment should integrate per-user authenticated sessions/key establishment using a maintained protocol/transport implementation.

## Verification and remaining acceptance work

Implemented automated checks include:

- Go and C# component tests for input bounds, prediction/reconciliation, reliable delivery, fragment atomicity, clock behavior, budgets, and packet protection.
- Equivalent planar movement at 7/20/30/60/120 server callbacks per second.
- 96 seeded server scenarios: RTT 0/50/150/300 ms, jitter 0/20/60 ms, loss 0/1/5/10%, and 1/5-second outages, including duplication/reordering.
- Session resumption, stale connection rejection, AOI boundary hysteresis, and 65-entity baseline fragmentation.
- Optional real UDP two-client tests, including seeded bidirectional impairment, in-session action deduplication, bounded recovery, and preserved identity.
- Go static analysis, Godot compilation, and headless scene/transport smoke checks.

Remaining acceptance work includes visual playtesting, representative entity/player load tests, other platforms, real Internet paths, combat/collision controllers, and public-deployment security integration. The 128-session admission cap is a resource bound, not a measured supported player count. No universal ping tolerance or production capacity is claimed.

## Deferred meshing

Meshing is outside this implementation at the user's request. A future design still needs stable world IDs, ownership epochs/fencing, an authority directory, coordinated transfer of state/input/action frontiers, failure recovery, and cross-region interaction rules. AOI is independent of ownership and does not itself implement meshing.
