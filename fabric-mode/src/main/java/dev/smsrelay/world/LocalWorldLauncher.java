package dev.smsrelay.world;

import dev.smsrelay.config.RelayConfig;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.gui.screen.Screen;
import net.minecraft.resource.DataConfiguration;
import net.minecraft.world.Difficulty;
import net.minecraft.world.GameMode;
import net.minecraft.world.GameRules;
import net.minecraft.world.gen.GeneratorOptions;
import net.minecraft.world.gen.WorldPresets;
import net.minecraft.world.level.LevelInfo;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Starts a normal client integrated world without any vanilla remote login.
 *
 * createAndStart is the same path used by the "create world" UI in 1.21.1.
 * It performs data-pack loading on the render thread, starts the integrated
 * server, and opens the regular client connection to it.
 */
public final class LocalWorldLauncher {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/world");

    private LocalWorldLauncher() {
    }

    public static void start(MinecraftClient client, Screen parent) {
        String worldName = RelayConfig.worldName();
        String saveName = saveName(client);
        LevelInfo levelInfo = new LevelInfo(
                worldName,
                GameMode.SURVIVAL,
                false,
                Difficulty.NORMAL,
                true,
                new GameRules(),
                DataConfiguration.SAFE_MODE);
        GeneratorOptions generatorOptions = new GeneratorOptions(0L, true, false);

        LOGGER.info("Booting local integrated world '{}' (save '{}')", worldName, saveName);
        client.createIntegratedServerLoader().createAndStart(
                saveName,
                levelInfo,
                generatorOptions,
                WorldPresets::createDemoOptions,
                parent);
    }

    private static String saveName(MinecraftClient client) {
        java.util.UUID uuid = client.getSession().getUuidOrNull();
        if (uuid != null) {
            return "smsrelay-" + uuid;
        }
        String username = client.getSession().getUsername();
        String safe = username == null ? "player" : username.replaceAll("[^A-Za-z0-9_.-]", "_");
        return "smsrelay-" + safe;
    }
}
