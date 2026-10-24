package dev.smsrelay.sync;

import com.google.gson.JsonObject;
import dev.smsrelay.config.RelayConfig;
import dev.smsrelay.network.RelayClient;
import dev.smsrelay.protocol.Channels;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.world.ClientWorld;
import net.minecraft.util.math.ChunkPos;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

/**
 * Owns the chunk-authority side of the relay contract.
 *
 * Current scope intentionally transfers a compact summary only, not a full
 * vanilla chunk serialization. A conflict therefore cannot be resolved by
 * replacing terrain yet; the manager requests the owner's payload, stores it,
 * and logs the limitation. Applying received chunk data is a documented TODO
 * because the relay format treats chunk data as opaque bytes.
 */
public final class ChunkAuthorityManager {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/chunk");

    private final MinecraftClient client;
    private final RelayClient relay;
    private final String worldId;
    private final Map<String, byte[]> summaries = new HashMap<>();
    private final Set<String> requested = new HashSet<>();
    private long lastClaimTick;

    public ChunkAuthorityManager(MinecraftClient client, RelayClient relay, String worldId) {
        this.client = client;
        this.relay = relay;
        this.worldId = worldId;
    }

    public void tick() {
        if (!relay.isRegistered()) {
            return;
        }
        if (client.player == null || client.world == null) {
            return;
        }
        long now = System.currentTimeMillis();
        if (now - lastClaimTick < RelayConfig.chunkClaimIntervalMs()) {
            return;
        }
        lastClaimTick = now;

        ChunkPos center = client.player.getChunkPos();
        for (int dx = -1; dx <= 1; dx++) {
            for (int dz = -1; dz <= 1; dz++) {
                claim(center.x + dx, center.z + dz);
            }
        }
    }

    private void claim(int x, int z) {
        String chunkId = chunkId(x, z);
        if (summaries.containsKey(chunkId)) {
            return;
        }
        byte[] summary = ("summary:" + chunkId).getBytes(StandardCharsets.UTF_8);
        JsonObject payload = new JsonObject();
        payload.addProperty("chunk_id", chunkId);
        payload.addProperty("summary", Base64.getEncoder().encodeToString(summary));
        relay.send(Channels.CHUNK_PRODUCED, payload);
    }

    public void onAuthority(JsonObject payload) {
        String chunkId = string(payload, "chunk_id");
        String status = string(payload, "status");
        if (chunkId == null || status == null) {
            return;
        }
        if ("accepted".equalsIgnoreCase(status)) {
            String summary = string(payload, "summary");
            summaries.put(chunkId, summary == null ? new byte[0] : Base64.getDecoder().decode(summary));
            requested.remove(chunkId);
        } else if ("conflict".equalsIgnoreCase(status)) {
            summaries.remove(chunkId);
            if (requested.add(chunkId)) {
                JsonObject request = new JsonObject();
                request.addProperty("chunk_id", chunkId);
                relay.send(Channels.CHUNK_REQUEST, request);
                LOGGER.info("Chunk {} is owned by {}; requested authority data", chunkId, string(payload, "owner_name"));
            }
        }
    }

    public void onRequest(JsonObject payload) {
        String chunkId = string(payload, "chunk_id");
        if (chunkId == null) {
            return;
        }
        byte[] summary = summaries.get(chunkId);
        if (summary == null) {
            return;
        }
        JsonObject response = new JsonObject();
        response.addProperty("chunk_id", chunkId);
        response.addProperty("data", Base64.getEncoder().encodeToString(summary));
        relay.send(Channels.CHUNK_RESPONSE, response);
    }

    public void onResponse(JsonObject payload) {
        String chunkId = string(payload, "chunk_id");
        String data = string(payload, "data");
        if (chunkId == null || data == null) {
            return;
        }
        byte[] decoded = Base64.getDecoder().decode(data);
        summaries.put(chunkId, decoded);
        requested.remove(chunkId);
        LOGGER.info("Received {} bytes of opaque chunk data for {} (terrain replacement TODO)", decoded.length, chunkId);
    }

    public void reset() {
        summaries.clear();
        requested.clear();
        lastClaimTick = 0;
    }

    private String chunkId(int x, int z) {
        ClientWorld world = client.world;
        String dimension = world == null ? worldId : world.getRegistryKey().getValue().getPath();
        return dimension + ":" + x + "," + z;
    }

    private static String string(JsonObject object, String key) {
        return object.has(key) && !object.get(key).isJsonNull() ? object.get(key).getAsString() : null;
    }
}
