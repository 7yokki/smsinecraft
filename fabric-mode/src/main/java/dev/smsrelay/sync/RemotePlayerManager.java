package dev.smsrelay.sync;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.mojang.authlib.GameProfile;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.network.ClientPlayNetworkHandler;
import net.minecraft.client.world.ClientWorld;
import net.minecraft.entity.Entity;
import net.minecraft.entity.EntityType;
import net.minecraft.entity.LivingEntity;
import net.minecraft.network.packet.s2c.play.EntitiesDestroyS2CPacket;
import net.minecraft.network.packet.s2c.play.EntitySpawnS2CPacket;
import net.minecraft.network.packet.s2c.play.PlayerListS2CPacket;
import net.minecraft.network.packet.s2c.play.PlayerRemoveS2CPacket;
import net.minecraft.registry.Registries;
import net.minecraft.server.network.ServerPlayerEntity;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;
import net.minecraft.util.Hand;
import net.minecraft.util.Identifier;
import net.minecraft.util.math.Vec3d;
import net.minecraft.world.GameMode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.Collections;
import java.util.EnumSet;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;

/**
 * Client-side relay player and entity injection.
 *
 * Remote players are inserted through the vanilla client play handlers:
 * a player-list entry is sent first, then a normal player spawn packet. This
 * keeps tab list, player names, skins, social interactions and the vanilla
 * player renderer on their normal code paths. They are still client-only and
 * the integrated server does not know about them.
 */
public final class RemotePlayerManager {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/entity");
    private static final int FIRST_REMOTE_ENTITY_ID = -100_000;

    private final MinecraftClient client;
    private final Map<UUID, RemotePlayer> players = new HashMap<>();
    private final Map<String, Entity> relayEntities = new HashMap<>();
    private int nextEntityId = FIRST_REMOTE_ENTITY_ID;

    public RemotePlayerManager(MinecraftClient client) {
        this.client = client;
    }

    public void reset() {
        ClientWorld world = client.world;
        if (world != null) {
            Iterator<RemotePlayer> iterator = players.values().iterator();
            while (iterator.hasNext()) {
                removePlayer(iterator.next(), false);
                iterator.remove();
            }
            for (Entity entity : relayEntities.values()) {
                removeEntity(entity);
            }
        }
        players.clear();
        relayEntities.clear();
        nextEntityId = FIRST_REMOTE_ENTITY_ID;
    }

    public void onPlayerList(JsonObject payload) {
        ClientWorld world = client.world;
        ClientPlayNetworkHandler handler = client.getNetworkHandler();
        if (world == null || handler == null) {
            return;
        }
        JsonArray array = payload.has("players") && payload.get("players").isJsonArray()
                ? payload.getAsJsonArray("players")
                : new JsonArray();
        UUID self = localUuid();
        Set<UUID> seen = new HashSet<>();

        for (JsonElement element : array) {
            if (!element.isJsonObject()) {
                continue;
            }
            JsonObject object = element.getAsJsonObject();
            UUID uuid = parseUuid(string(object, "id"));
            if (uuid == null || uuid.equals(self)) {
                continue;
            }
            seen.add(uuid);
            String username = username(object, uuid);
            GameMode gameMode = gameMode(string(object, "game_mode"));
            RemotePlayer remote = players.get(uuid);
            if (remote == null) {
                spawnPlayer(world, handler, uuid, username, gameMode, null);
            } else if (!remote.username.equals(username)) {
                removePlayer(remote, true);
                spawnPlayer(world, handler, uuid, username, gameMode, null);
            }
        }

        Iterator<RemotePlayer> iterator = players.values().iterator();
        while (iterator.hasNext()) {
            RemotePlayer remote = iterator.next();
            if (!seen.contains(remote.uuid)) {
                removePlayer(remote, true);
                iterator.remove();
            }
        }
    }

    public void onPosition(JsonObject payload) {
        ClientWorld world = client.world;
        ClientPlayNetworkHandler handler = client.getNetworkHandler();
        if (world == null || handler == null) {
            return;
        }
        UUID uuid = parseUuid(string(payload, "player_id"));
        if (uuid == null || uuid.equals(localUuid())) {
            return;
        }
        double x = number(payload, "x", 0.0);
        double y = number(payload, "y", 64.0);
        double z = number(payload, "z", 0.0);
        float yaw = (float) number(payload, "yaw", 0.0);
        float pitch = (float) number(payload, "pitch", 0.0);
        String username = username(payload, uuid);
        RemotePlayer remote = players.get(uuid);
        if (remote == null) {
            remote = spawnPlayer(world, handler, uuid, username, GameMode.SURVIVAL, new Position(x, y, z, yaw, pitch));
        } else if (!remote.username.equals(username)) {
            removePlayer(remote, true);
            remote = spawnPlayer(world, handler, uuid, username, remote.gameMode, new Position(x, y, z, yaw, pitch));
        }
        if (remote == null) {
            return;
        }
        Entity entity = world.getEntityById(remote.entityId);
        if (entity == null) {
            return;
        }
        updateTracked(entity, x, y, z, yaw, pitch);
    }

    public void onEntityUpdate(JsonObject payload) {
        ClientWorld world = client.world;
        if (world == null) {
            return;
        }
        String relayId = string(payload, "entity_id");
        if (relayId == null || relayId.isBlank()) {
            return;
        }
        String typeId = string(payload, "type");
        if (typeId == null || typeId.isBlank()) {
            typeId = "minecraft:armor_stand";
        }
        if (!typeId.contains(":")) {
            typeId = "minecraft:" + typeId;
        }
        if (typeId.equals("minecraft:player")) {
            return;
        }

        Entity entity = relayEntities.get(relayId);
        if (entity == null) {
            Identifier identifier = Identifier.tryParse(typeId);
            if (identifier == null) {
                return;
            }
            EntityType<?> type = Registries.ENTITY_TYPE.get(identifier);
            Entity created = type.create(world);
            if (created == null) {
                return;
            }
            created.setId(nextEntityId--);
            created.refreshPositionAndAngles(
                    number(payload, "x", 0),
                    number(payload, "y", 64),
                    number(payload, "z", 0),
                    0.0F, 0.0F);
            world.addEntity(created);
            relayEntities.put(relayId, created);
            entity = created;
        } else {
            entity.refreshPositionAndAngles(
                    number(payload, "x", entity.getX()),
                    number(payload, "y", entity.getY()),
                    number(payload, "z", entity.getZ()),
                    entity.getYaw(), entity.getPitch());
        }
    }

    public void onAttack(JsonObject payload) {
        ClientWorld world = client.world;
        if (world == null) {
            return;
        }
        UUID actor = parseUuid(string(payload, "actor_id"));
        if (actor != null) {
            RemotePlayer attacker = players.get(actor);
            Entity entity = attacker == null ? null : world.getEntityById(attacker.entityId);
            if (entity instanceof LivingEntity livingEntity) {
                livingEntity.swingHand(Hand.MAIN_HAND);
            }
        }
        String target = string(payload, "target_entity_id");
        if (target != null) {
            for (RemotePlayer remote : players.values()) {
                if (remote.uuid.toString().equals(target)) {
                    Entity entity = world.getEntityById(remote.entityId);
                    if (entity instanceof LivingEntity livingEntity) {
                        livingEntity.swingHand(Hand.MAIN_HAND);
                    }
                    break;
                }
            }
            Entity entity = relayEntities.get(target);
            if (entity != null) {
                entity.handleStatus((byte) 2);
            }
        }
    }

    public String relayIdForEntity(int entityId) {
        for (RemotePlayer remote : players.values()) {
            if (remote.entityId == entityId) {
                return remote.uuid.toString();
            }
        }
        for (Map.Entry<String, Entity> entry : relayEntities.entrySet()) {
            if (entry.getValue().getId() == entityId) {
                return entry.getKey();
            }
        }
        return "";
    }

    public boolean isRemotePlayer(int entityId) {
        for (RemotePlayer remote : players.values()) {
            if (remote.entityId == entityId) {
                return true;
            }
        }
        return false;
    }

    private RemotePlayer spawnPlayer(
            ClientWorld world,
            ClientPlayNetworkHandler handler,
            UUID uuid,
            String username,
            GameMode gameMode,
            Position position) {
        addPlayerListEntry(handler, uuid, username, gameMode);
        int entityId = nextEntityId--;
        Vec3d spawn = position == null
                ? Vec3d.ofBottomCenter(world.getSpawnPos())
                : new Vec3d(position.x, position.y, position.z);
        float yaw = position == null ? 0.0F : position.yaw;
        float pitch = position == null ? 0.0F : position.pitch;
        handler.onEntitySpawn(new EntitySpawnS2CPacket(
                entityId, uuid, spawn.x, spawn.y, spawn.z, pitch, yaw,
                EntityType.PLAYER, 0, Vec3d.ZERO, yaw));
        RemotePlayer remote = new RemotePlayer(uuid, username, entityId, gameMode);
        players.put(uuid, remote);
        Entity entity = world.getEntityById(entityId);
        if (entity != null) {
            updateTracked(entity, spawn.x, spawn.y, spawn.z, yaw, pitch);
        }
        LOGGER.info("Injected remote player {} ({}) as entity {}", username, uuid, entityId);
        announce(username + " bağlandı");
        return remote;
    }

    private void addPlayerListEntry(ClientPlayNetworkHandler handler, UUID uuid, String username, GameMode gameMode) {
        EnumSet<PlayerListS2CPacket.Action> actions = EnumSet.of(
                PlayerListS2CPacket.Action.ADD_PLAYER,
                PlayerListS2CPacket.Action.UPDATE_GAME_MODE,
                PlayerListS2CPacket.Action.UPDATE_LISTED,
                PlayerListS2CPacket.Action.UPDATE_LATENCY,
                PlayerListS2CPacket.Action.UPDATE_DISPLAY_NAME);
        List<PlayerListS2CPacket.Entry> entries = List.of(new PlayerListS2CPacket.Entry(
                uuid,
                new GameProfile(uuid, username),
                true,
                0,
                gameMode,
                Text.literal(username),
                null));
        PlayerListS2CPacket packet = new PlayerListS2CPacket(
                actions, Collections.<ServerPlayerEntity>emptyList()) {
            @Override
            public EnumSet<PlayerListS2CPacket.Action> getActions() {
                return actions;
            }

            @Override
            public List<PlayerListS2CPacket.Entry> getEntries() {
                return entries;
            }

            @Override
            public List<PlayerListS2CPacket.Entry> getPlayerAdditionEntries() {
                return entries;
            }
        };
        handler.onPlayerList(packet);
    }

    private void removePlayer(RemotePlayer remote, boolean announce) {
        ClientPlayNetworkHandler handler = client.getNetworkHandler();
        if (handler != null) {
            handler.onEntitiesDestroy(new EntitiesDestroyS2CPacket(remote.entityId));
            handler.onPlayerRemove(new PlayerRemoveS2CPacket(List.of(remote.uuid)));
        }
        if (announce) {
            announce(remote.username + " ayrıldı");
        }
    }

    private void removeEntity(Entity entity) {
        ClientWorld world = client.world;
        if (world != null) {
            world.removeEntity(entity.getId(), Entity.RemovalReason.DISCARDED);
        }
    }

    private void announce(String message) {
        if (client.player != null && client.inGameHud != null) {
            client.inGameHud.getChatHud().addMessage(Text.literal(message).formatted(Formatting.YELLOW));
        }
    }

    private static void updateTracked(Entity entity, double x, double y, double z, float yaw, float pitch) {
        entity.updateTrackedPositionAndAngles(x, y, z, yaw, pitch, 3);
        entity.updateTrackedHeadRotation(yaw, 3);
    }

    private UUID localUuid() {
        if (client.player != null) {
            return client.player.getUuid();
        }
        return client.getSession().getUuidOrNull();
    }

    private static String username(JsonObject object, UUID uuid) {
        String username = string(object, "username");
        if (username == null || username.isBlank()) {
            username = "player-" + uuid.toString().substring(0, 6);
        }
        return username;
    }

    private static GameMode gameMode(String name) {
        return name == null || name.isBlank() ? GameMode.SURVIVAL : GameMode.byName(name, GameMode.SURVIVAL);
    }

    private static UUID parseUuid(String value) {
        if (value == null || value.isBlank()) {
            return null;
        }
        try {
            return UUID.fromString(value);
        } catch (IllegalArgumentException e) {
            return null;
        }
    }

    private static String string(JsonObject object, String key) {
        return object.has(key) && !object.get(key).isJsonNull() ? object.get(key).getAsString() : null;
    }

    private static double number(JsonObject object, String key, double fallback) {
        try {
            return object.has(key) && !object.get(key).isJsonNull() ? object.get(key).getAsDouble() : fallback;
        } catch (RuntimeException e) {
            return fallback;
        }
    }

    private record RemotePlayer(UUID uuid, String username, int entityId, GameMode gameMode) {
    }

    private record Position(double x, double y, double z, float yaw, float pitch) {
    }
}
