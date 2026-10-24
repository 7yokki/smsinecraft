package dev.smsrelay;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import dev.smsrelay.config.RelayConfig;
import dev.smsrelay.network.RelayClient;
import dev.smsrelay.protocol.Channels;
import dev.smsrelay.sync.ChunkAuthorityManager;
import dev.smsrelay.sync.RemotePlayerManager;
import dev.smsrelay.sync.WorldSync;
import dev.smsrelay.world.LocalWorldLauncher;
import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientTickEvents;
import net.minecraft.block.BlockState;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.gui.screen.Screen;
import net.minecraft.client.network.ClientPlayerEntity;
import net.minecraft.registry.Registries;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;
import net.minecraft.util.math.BlockPos;
import net.minecraft.world.GameMode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.UUID;

public final class SmsRelayMod implements ClientModInitializer {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay");
    private static final long RECONNECT_DELAY_MS = 5000L;
    private static final long CLIENT_PING_INTERVAL_MS = 20_000L;

    private static SmsRelayMod instance;

    private boolean sessionActive;
    private boolean fatalError;
    private RelayClient relay;
    private RemotePlayerManager remotePlayers;
    private WorldSync worldSync;
    private ChunkAuthorityManager chunkAuthority;
    private long lastPositionSend;
    private long lastPingSend;
    private long lastReconnectAttempt;
    private boolean initialStateSent;
    private boolean deathAnnounced;

    public static SmsRelayMod getInstance() {
        return instance;
    }

    public boolean isSessionActive() {
        return sessionActive;
    }

    @Override
    public void onInitializeClient() {
        instance = this;
        RelayConfig.load();
        ClientTickEvents.END_CLIENT_TICK.register(this::onClientTick);
        LOGGER.info("SMS Relay client initialized");
    }

    public void beginRelaySession(MinecraftClient client, Screen parent) {
        sessionActive = true;
        fatalError = false;
        initialStateSent = false;
        shutdownRelay("starting new session");
        LocalWorldLauncher.start(client, parent);
    }

    private void onClientTick(MinecraftClient client) {
        if (!sessionActive) {
            return;
        }
        if (client.world == null || client.player == null) {
            if (relay != null) {
                shutdownRelay("local world closed");
                sessionActive = false;
            }
            return;
        }

        if (client.player.isDead()) {
            if (!deathAnnounced) {
                deathAnnounced = true;
                notifyChat("Öldünüz");
            }
        } else {
            deathAnnounced = false;
        }

        if (relay == null && !fatalError) {
            long now = System.currentTimeMillis();
            if (now - lastReconnectAttempt >= RECONNECT_DELAY_MS) {
                lastReconnectAttempt = now;
                startRelay(client);
            }
        }

        if (relay == null) {
            return;
        }

        processInbound(client);
        if (relay != null && !relay.isRunning() && RelayConfig.autoReconnect() && !fatalError) {
            LOGGER.info("Relay disconnected; scheduling reconnect");
            notifyChat("Relay bağlantısı kesildi");
            shutdownRelay("connection lost");
        }
        if (relay == null || !relay.isRegistered()) {
            return;
        }

        long now = System.currentTimeMillis();
        if (worldSync == null) {
            worldSync = new WorldSync(client, localWorldId(client));
        }
        if (!initialStateSent) {
            sendWorldState(client);
            initialStateSent = true;
        }
        if (now - lastPositionSend >= RelayConfig.positionIntervalMs()) {
            sendPosition(client);
            lastPositionSend = now;
        }
        if (now - lastPingSend >= CLIENT_PING_INTERVAL_MS) {
            JsonObject ping = new JsonObject();
            ping.addProperty("time", now);
            relay.send(Channels.PING, ping);
            lastPingSend = now;
        }
        if (chunkAuthority != null) {
            chunkAuthority.tick();
        }
    }

    private void startRelay(MinecraftClient client) {
        ClientPlayerEntity player = client.player;
        if (player == null) {
            return;
        }
        UUID uuid = player.getUuid();
        String username = player.getGameProfile().getName();
        GameMode gameMode = client.interactionManager == null
                ? GameMode.SURVIVAL
                : client.interactionManager.getCurrentGameMode();
        String world = localWorldId(client);

        remotePlayers = new RemotePlayerManager(client);
        relay = new RelayClient(RelayConfig.host(), RelayConfig.port(), RelayConfig.maxFrameBytes());
        chunkAuthority = new ChunkAuthorityManager(client, relay, world);
        relay.start(uuid.toString(), username, gameMode.getName(), world);
        LOGGER.info("Connecting relay as {} ({}) in {}", username, uuid, world);
    }

    private void processInbound(MinecraftClient client) {
        RelayClient.Inbound inbound;
        while (relay != null && (inbound = relay.pollInbound()) != null) {
            JsonObject payload = inbound.payload();
            switch (inbound.channel()) {
                case Channels.CONNECT_ACK -> {
                    LOGGER.info("Relay accepted player {}", payload);
                    notifyChat("Relay sunucusuna bağlandı");
                    lastPositionSend = 0;
                    lastPingSend = 0;
                }
                case Channels.PLAYER_LIST -> {
                    if (remotePlayers != null) {
                        remotePlayers.onPlayerList(payload);
                    }
                }
                case Channels.POSITION -> {
                    if (remotePlayers != null) {
                        remotePlayers.onPosition(payload);
                    }
                }
                case Channels.BLOCK_LOG -> {
                    if (worldSync != null && payload.has("events") && payload.get("events").isJsonArray()) {
                        JsonArray events = payload.getAsJsonArray("events");
                        for (int i = 0; i < events.size(); i++) {
                            if (events.get(i).isJsonObject()) {
                                worldSync.onBlockChange(events.get(i).getAsJsonObject());
                            }
                        }
                    }
                }
                case Channels.BLOCK_CHANGE -> {
                    if (worldSync != null) {
                        worldSync.onBlockChange(payload);
                    }
                }
                case Channels.WORLD_STATE -> {
                    if (worldSync != null) {
                        worldSync.onWorldState(payload);
                    }
                }
                case Channels.ENTITY_UPDATE -> {
                    if (remotePlayers != null) {
                        remotePlayers.onEntityUpdate(payload);
                    }
                }
                case Channels.ATTACK -> {
                    if (remotePlayers != null) {
                        remotePlayers.onAttack(payload);
                    }
                }
                case Channels.CORRECTION -> {
                    if (worldSync != null) {
                        worldSync.onCorrection(payload);
                    }
                }
                case Channels.CHUNK_AUTHORITY -> {
                    if (chunkAuthority != null) {
                        chunkAuthority.onAuthority(payload);
                    }
                }
                case Channels.CHUNK_REQUEST -> {
                    if (chunkAuthority != null) {
                        chunkAuthority.onRequest(payload);
                    }
                }
                case Channels.CHUNK_RESPONSE -> {
                    if (chunkAuthority != null) {
                        chunkAuthority.onResponse(payload);
                    }
                }
                case Channels.ERROR -> {
                    if (worldSync != null) {
                        worldSync.onError(payload);
                    }
                    String code = payload.has("code") ? payload.get("code").getAsString() : "";
                    if ("duplicate_uuid".equals(code)) {
                        fatalError = true;
                        notifyChat("Relay: bu oyuncu kimliği zaten bağlı");
                    }
                }
                case Channels.PONG -> {
                }
                default -> LOGGER.debug("Unhandled relay channel {}", inbound.channel());
            }
        }
    }

    public void onLocalBlockChange(BlockPos pos, BlockState state) {
        if (relay == null || !relay.isRegistered()) {
            return;
        }
        String blockId = Registries.BLOCK.getId(state.getBlock()).toString();
        JsonObject payload = new JsonObject();
        payload.addProperty("world", localWorldId(MinecraftClient.getInstance()));
        payload.addProperty("x", pos.getX());
        payload.addProperty("y", pos.getY());
        payload.addProperty("z", pos.getZ());
        payload.addProperty("block_id", blockId);
        relay.send(Channels.BLOCK_CHANGE, payload);
    }

    public void onLocalAttack(int targetEntityId, double x, double y, double z) {
        if (relay == null || !relay.isRegistered()) {
            return;
        }
        JsonObject payload = new JsonObject();
        String target = remotePlayers == null ? "" : remotePlayers.relayIdForEntity(targetEntityId);
        payload.addProperty("target_entity_id", target);
        payload.addProperty("x", x);
        payload.addProperty("y", y);
        payload.addProperty("z", z);
        relay.send(Channels.ATTACK, payload);
    }

    private void sendPosition(MinecraftClient client) {
        ClientPlayerEntity player = client.player;
        if (player == null || relay == null) {
            return;
        }
        JsonObject payload = new JsonObject();
        payload.addProperty("x", player.getX());
        payload.addProperty("y", player.getY());
        payload.addProperty("z", player.getZ());
        payload.addProperty("yaw", player.getYaw());
        payload.addProperty("pitch", player.getPitch());
        payload.addProperty("on_ground", player.isOnGround());
        relay.send(Channels.POSITION, payload);
    }

    private void sendWorldState(MinecraftClient client) {
        if (client.world == null || relay == null) {
            return;
        }
        JsonObject payload = new JsonObject();
        payload.addProperty("time_of_day", client.world.getTimeOfDay());
        payload.addProperty("weather", weatherName(client));
        BlockPos spawn = client.world.getSpawnPos();
        payload.addProperty("spawn_x", spawn.getX() + 0.5);
        payload.addProperty("spawn_y", spawn.getY());
        payload.addProperty("spawn_z", spawn.getZ() + 0.5);
        payload.addProperty("border_diameter", client.world.getWorldBorder().getSize());
        relay.send(Channels.WORLD_STATE, payload);
    }

    private static String weatherName(MinecraftClient client) {
        if (client.world == null) {
            return "clear";
        }
        if (client.world.getThunderGradient(1.0F) > 0.5F) {
            return "thunder";
        }
        if (client.world.getRainGradient(1.0F) > 0.5F) {
            return "rain";
        }
        return "clear";
    }

    private static String localWorldId(MinecraftClient client) {
        if (client.world != null) {
            return client.world.getRegistryKey().getValue().getPath();
        }
        return RelayConfig.worldId();
    }

    public void notifyChat(String message) {
        MinecraftClient client = MinecraftClient.getInstance();
        if (client.player != null && client.inGameHud != null) {
            client.inGameHud.getChatHud().addMessage(Text.literal(message).formatted(Formatting.YELLOW));
        }
    }

    private void shutdownRelay(String reason) {
        boolean wasRegistered = relay != null && relay.isRegistered();
        if (relay != null) {
            relay.stop(reason);
            relay = null;
        }
        if (wasRegistered) {
            notifyChat("Relay bağlantısı kesildi");
        }
        if (chunkAuthority != null) {
            chunkAuthority.reset();
            chunkAuthority = null;
        }
        if (remotePlayers != null) {
            remotePlayers.reset();
            remotePlayers = null;
        }
        worldSync = null;
        initialStateSent = false;
    }
}
