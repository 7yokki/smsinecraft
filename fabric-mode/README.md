# SMS Relay Fabric Client

Client-side Fabric mod for the Go relay in `../`. It does **not** modify the
relay. When the player opens any vanilla multiplayer connection, the mod
cancels it, starts a normal local integrated world, and connects that client to
the relay over a separate TCP socket.

## Toolchain

Pinned in `gradle.properties` and the Gradle wrapper:

| Component | Version |
|---|---|
| Minecraft | 1.21.1 |
| Yarn mappings | 1.21.1+build.3 |
| Fabric Loader | 0.16.14 |
| Fabric API | 0.116.17+1.21.1 |
| Fabric Loom | 1.7.4 |
| Gradle wrapper | 8.10.2 |
| Java (Gradle + mod target) | 21 |

This combination was chosen because Loom 1.18.x requires Gradle 9.7+/Java 25,
while Loom 1.7.4 is the stable line for Minecraft 1.21.1 and runs on Java 21.
The source and bytecode target are Java 21.

## Build

```bash
cd fabric-mod
./gradlew build
./gradlew test
```

Outputs:

- `build/libs/sms-relay-fabric-0.1.0.jar`
- `build/libs/sms-relay-fabric-0.1.0-sources.jar`

Install the remapped jar plus Fabric API for 1.21.1 into the client `mods/`
folder. The mod is client-only (`environment: client`).

## Configuration

On first launch the mod writes `config/smsrelay.properties`:

```properties
relay_host=127.0.0.1
relay_port=25566
intercept_all_multiplayer=true
auto_reconnect=true
world_name=smsrelay
world_id=overworld
max_frame_bytes=2097152
position_interval_ms=50
chunk_claim_interval_ms=5000
```

Set `intercept_all_multiplayer=false` to keep vanilla multiplayer untouched.

## Implemented behavior

- Exact big-endian relay framing and channel names.
- Dedicated relay reader/writer threads. Framing and JSON parsing never run on
  the Minecraft tick thread; world/entity changes are applied from
  `ClientTickEvents.END_CLIENT_TICK`.
- Ordered handshake: `sms:connect` is the first frame written after connect.
- Inbound: `connect_ack`, `block_log`, `world_state`, `player_list`, `position`,
  `block_change`, `chunk_authority`, `chunk_request`, `chunk_response`,
  `entity_update`, `attack`, `correction`, `error`, `ping`, `pong`.
- Outbound: `connect`, `disconnect`, `position`, `block_change`, `world_state`,
  `chunk_produced`, `chunk_request`, `chunk_response`, `attack`, `ping`, `pong`.
- Remote players are injected through the vanilla play handlers: a
  `PlayerListS2CPacket` entry is applied first, then a normal `PLAYER`
  `EntitySpawnS2CPacket`. This keeps the vanilla tab list, player renderer,
  name tags, skins and social-interactions data on their normal code paths.
- While a relay session is active, the integrated world is not paused when a
  screen opens. Vanilla's pause checks are redirected for the relay session, so
  the server keeps ticking like a multiplayer world.
- Local chat messages are emitted for relay connect/disconnect, remote
  player join/leave, duplicate UUID errors, and local player death.
- Non-player relay entities are instantiated through
  `Registries.ENTITY_TYPE` when the type has a client factory.
- Block break/placement is observed through
  `ClientPlayerInteractionManager` mixins; attacks through `attackEntity`.
- Relay blocks, time, weather, world border and corrections are applied locally.
- Chunk claims are summarized (base64, far below 2 KiB) for a 3x3 area.

## Experimental / risky assumptions

These are the parts most likely to need changes after a live multi-client test:

1. **Connection interception.** `ConnectScreenMixin` cancels the static
   `ConnectScreen.connect(...)` entry point. This relies on the 1.21.1 Yarn
   method descriptor and is remapped via the generated refmap. If a future
   Minecraft version moves the quick-play/multiplayer entry point, this must be
   revisited.
2. **Fake player-list entries and spawn packets.** Remote players go through
   `ClientPlayNetworkHandler.onPlayerList` and `onEntitySpawn`, but the packet
   is constructed locally with a small anonymous `PlayerListS2CPacket`
   subclass because 1.21.1 exposes no public entry-list constructor. They are
   still client-only, are not saved, and the integrated server is unaware of
   them. Their IDs are negative to avoid collisions with integrated-server
   entities. Vanilla command targeting (`/tp <remote>`) therefore does not see
   them. Rendering/name tags/skins/tab list have not been verified with two real
   clients yet.
3. **Local world start.** `IntegratedServerLoader.createAndStart` is the 1.21.1
   internal "create world" path. It is not a stable public API. Each account
   gets its own save directory (`smsrelay-<uuid>`) so two client instances do
   not fight over `session.lock`; the display name is still the configured
   `world_name`. A fixed seed is used.
4. **Correction mirroring.** A relay correction is applied to the client player
   and, when available, teleported on the integrated server's `ServerPlayerEntity`.
   The server-side lookup is internal API and should be tested against an actual
   integrated-server tick.
5. **Block placement detection.** `interactBlock` snapshots the hit block and
   the adjacent placement block, then compares states after the call. This
   catches normal placement/use changes but not every custom item or multi-block
   interaction.

## Deliberate TODOs

- **Chunk payloads are summaries, not terrain.** Per the current scope, only a
  compact opaque summary is produced and relayed. On `chunk_authority=conflict`
  the mod requests the owner's payload and stores it, but cannot yet replace the
  local vanilla chunk because no full chunk serialization format exists. This is
  the largest remaining functional gap for true cross-client terrain ownership.
- Entity authority is not claimed for local mobs; only remote relay entities are
  rendered.
- Remote held/armor items and dropped item stacks cannot be synchronized yet:
  the current relay schema has no equipment or item-stack fields. Adding them
  requires an additive relay protocol change.
- Remote death messages and shared chat are not relayed by the current schema.
  Death is currently announced locally only.
- `/tp <remote>` and other server-side commands cannot target remote players
  because they do not exist in the integrated server's `PlayerManager`.
- No authentication, TLS, encryption, or vanilla login flow (by design).
- No custom skin fetching; remote players use the default UUID skin.
- Runtime startup and two-client behavior were not manually exercised in an
  interactive Minecraft client in this build environment. Compilation, mixin
  refmap generation and protocol unit tests pass.

## Protocol tests

`src/test/java/dev/smsrelay/protocol/RelayFrameTest.java` verifies the exact Go
frame layout, a byte-for-byte golden frame, round trips, bad magic, oversized
frames and invalid channels.
