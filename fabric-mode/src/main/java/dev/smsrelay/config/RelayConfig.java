package dev.smsrelay.config;

import net.fabricmc.loader.api.FabricLoader;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.io.Reader;
import java.io.Writer;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Properties;

public final class RelayConfig {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/config");
    private static final String FILE_NAME = "smsrelay.properties";

    private static String host = "127.0.0.1";
    private static int port = 25566;
    private static boolean interceptAllMultiplayer = true;
    private static boolean autoReconnect = true;
    private static String worldName = "smsrelay";
    private static String worldId = "overworld";
    private static int maxFrameBytes = 2 * 1024 * 1024;
    private static int positionIntervalMs = 50;
    private static int chunkClaimIntervalMs = 5000;

    private RelayConfig() {
    }

    public static synchronized void load() {
        Path path = FabricLoader.getInstance().getConfigDir().resolve(FILE_NAME);
        Properties properties = new Properties();
        if (Files.isRegularFile(path)) {
            try (Reader reader = Files.newBufferedReader(path)) {
                properties.load(reader);
            } catch (IOException e) {
                LOGGER.warn("Could not read {}, using defaults", path, e);
            }
        } else {
            writeDefaults(path, properties);
        }

        host = properties.getProperty("relay_host", host).trim();
        port = parseInt(properties, "relay_port", port);
        interceptAllMultiplayer = parseBoolean(properties, "intercept_all_multiplayer", interceptAllMultiplayer);
        autoReconnect = parseBoolean(properties, "auto_reconnect", autoReconnect);
        worldName = properties.getProperty("world_name", worldName).trim();
        worldId = properties.getProperty("world_id", worldId).trim();
        maxFrameBytes = parseInt(properties, "max_frame_bytes", maxFrameBytes);
        positionIntervalMs = parseInt(properties, "position_interval_ms", positionIntervalMs);
        chunkClaimIntervalMs = parseInt(properties, "chunk_claim_interval_ms", chunkClaimIntervalMs);

        if (host.isEmpty()) {
            host = "127.0.0.1";
        }
        if (port < 1 || port > 65535) {
            port = 25566;
        }
        if (worldName.isEmpty()) {
            worldName = "smsrelay";
        }
        if (worldId.isEmpty()) {
            worldId = "overworld";
        }
        if (maxFrameBytes < 1024) {
            maxFrameBytes = 2 * 1024 * 1024;
        }
        positionIntervalMs = Math.max(20, positionIntervalMs);
        chunkClaimIntervalMs = Math.max(1000, chunkClaimIntervalMs);

        LOGGER.info("Relay config: {}:{}, interceptAll={}, world={}", host, port, interceptAllMultiplayer, worldName);
    }

    private static void writeDefaults(Path path, Properties properties) {
        properties.setProperty("relay_host", host);
        properties.setProperty("relay_port", Integer.toString(port));
        properties.setProperty("intercept_all_multiplayer", Boolean.toString(interceptAllMultiplayer));
        properties.setProperty("auto_reconnect", Boolean.toString(autoReconnect));
        properties.setProperty("world_name", worldName);
        properties.setProperty("world_id", worldId);
        properties.setProperty("max_frame_bytes", Integer.toString(maxFrameBytes));
        properties.setProperty("position_interval_ms", Integer.toString(positionIntervalMs));
        properties.setProperty("chunk_claim_interval_ms", Integer.toString(chunkClaimIntervalMs));
        try {
            Files.createDirectories(path.getParent());
            try (Writer writer = Files.newBufferedWriter(path)) {
                properties.store(writer, "SMS Relay Fabric client configuration");
            }
        } catch (IOException e) {
            LOGGER.warn("Could not write default config {}", path, e);
        }
    }

    private static int parseInt(Properties properties, String key, int fallback) {
        try {
            return Integer.parseInt(properties.getProperty(key, Integer.toString(fallback)).trim());
        } catch (NumberFormatException e) {
            return fallback;
        }
    }

    private static boolean parseBoolean(Properties properties, String key, boolean fallback) {
        String value = properties.getProperty(key);
        return value == null ? fallback : Boolean.parseBoolean(value.trim());
    }

    public static String host() {
        return host;
    }

    public static int port() {
        return port;
    }

    public static boolean interceptAllMultiplayer() {
        return interceptAllMultiplayer;
    }

    public static boolean autoReconnect() {
        return autoReconnect;
    }

    public static String worldName() {
        return worldName;
    }

    public static String worldId() {
        return worldId;
    }

    public static int maxFrameBytes() {
        return maxFrameBytes;
    }

    public static int positionIntervalMs() {
        return positionIntervalMs;
    }

    public static int chunkClaimIntervalMs() {
        return chunkClaimIntervalMs;
    }
}
