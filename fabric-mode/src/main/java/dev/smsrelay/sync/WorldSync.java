package dev.smsrelay.sync;

import com.google.gson.JsonObject;
import net.minecraft.block.Block;
import net.minecraft.block.BlockState;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.network.ClientPlayerEntity;
import net.minecraft.client.world.ClientWorld;
import net.minecraft.registry.Registries;
import net.minecraft.server.network.ServerPlayerEntity;
import net.minecraft.server.world.ServerWorld;
import net.minecraft.util.Identifier;
import net.minecraft.util.math.BlockPos;
import net.minecraft.util.math.Vec3d;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public final class WorldSync {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/world");

    private final MinecraftClient client;
    private final String localWorldId;

    public WorldSync(MinecraftClient client, String localWorldId) {
        this.client = client;
        this.localWorldId = localWorldId;
    }

    public void onBlockChange(JsonObject payload) {
        ClientWorld world = client.world;
        if (world == null) {
            return;
        }
        String worldId = string(payload, "world");
        if (worldId != null && !worldId.isBlank() && !worldId.equals(localWorldId)
                && !worldId.equals(world.getRegistryKey().getValue().toString())) {
            return;
        }
        String blockId = string(payload, "block_id");
        if (blockId == null || blockId.isBlank()) {
            return;
        }
        Identifier identifier = Identifier.tryParse(blockId);
        if (identifier == null) {
            return;
        }
        Block block = Registries.BLOCK.get(identifier);
        BlockState state = block.getDefaultState();
        BlockPos pos = new BlockPos(intValue(payload, "x"), intValue(payload, "y"), intValue(payload, "z"));
        world.setBlockState(pos, state, Block.NOTIFY_ALL);
    }

    public void onWorldState(JsonObject payload) {
        ClientWorld world = client.world;
        if (world == null) {
            return;
        }
        long timeOfDay = longValue(payload, "time_of_day", world.getTimeOfDay());
        world.setTimeOfDay(timeOfDay);

        double diameter = doubleValue(payload, "border_diameter", world.getWorldBorder().getSize());
        if (diameter > 0 && Double.isFinite(diameter)) {
            double spawnX = doubleValue(payload, "spawn_x", world.getWorldBorder().getCenterX());
            double spawnZ = doubleValue(payload, "spawn_z", world.getWorldBorder().getCenterZ());
            if (Double.isFinite(spawnX) && Double.isFinite(spawnZ)) {
                world.getWorldBorder().setCenter(spawnX, spawnZ);
            }
            world.getWorldBorder().setSize(diameter);
        }

        String weather = string(payload, "weather");
        if (weather != null) {
            switch (weather.toLowerCase()) {
                case "rain", "raining", "rainy" -> {
                    world.setRainGradient(1.0F);
                    world.setThunderGradient(0.0F);
                }
                case "thunder", "storm", "thunderstorm" -> {
                    world.setRainGradient(1.0F);
                    world.setThunderGradient(1.0F);
                }
                case "clear", "sunny" -> {
                    world.setRainGradient(0.0F);
                    world.setThunderGradient(0.0F);
                }
                default -> {
                }
            }
        }
    }

    public void onCorrection(JsonObject payload) {
        ClientPlayerEntity player = client.player;
        if (player == null) {
            return;
        }
        double x = doubleValue(payload, "x", player.getX());
        double y = doubleValue(payload, "y", player.getY());
        double z = doubleValue(payload, "z", player.getZ());
        float yaw = (float) doubleValue(payload, "yaw", player.getYaw());
        float pitch = (float) doubleValue(payload, "pitch", player.getPitch());

        player.refreshPositionAndAngles(x, y, z, yaw, pitch);
        player.setVelocity(Vec3d.ZERO);
        LOGGER.info("Relay correction applied at {}, {}, {} ({})", x, y, z, string(payload, "reason"));

        if (client.getServer() != null) {
            ServerPlayerEntity serverPlayer = client.getServer().getPlayerManager().getPlayer(player.getUuid());
            if (serverPlayer != null) {
                try {
                    serverPlayer.teleport((ServerWorld) serverPlayer.getWorld(), x, y, z, yaw, pitch);
                } catch (RuntimeException e) {
                    LOGGER.warn("Could not mirror correction to integrated server player", e);
                }
            }
        }
    }

    public void onError(JsonObject payload) {
        String code = string(payload, "code");
        String message = string(payload, "message");
        String channel = string(payload, "channel");
        LOGGER.warn("Relay error [{}] on {}: {}", code, channel, message);
        if (client.player != null) {
            client.player.sendMessage(net.minecraft.text.Text.literal(
                    "SMS relay error [" + code + "]: " + message), false);
        }
    }

    private static String string(JsonObject object, String key) {
        return object.has(key) && !object.get(key).isJsonNull() ? object.get(key).getAsString() : null;
    }

    private static int intValue(JsonObject object, String key) {
        try {
            return object.get(key).getAsInt();
        } catch (RuntimeException e) {
            return 0;
        }
    }

    private static long longValue(JsonObject object, String key, long fallback) {
        try {
            return object.get(key).getAsLong();
        } catch (RuntimeException e) {
            return fallback;
        }
    }

    private static double doubleValue(JsonObject object, String key, double fallback) {
        try {
            double value = object.get(key).getAsDouble();
            return Double.isFinite(value) ? value : fallback;
        } catch (RuntimeException e) {
            return fallback;
        }
    }
}
