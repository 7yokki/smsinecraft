package dev.smsrelay.mixin;

import dev.smsrelay.SmsRelayMod;
import net.minecraft.client.MinecraftClient;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Redirect;

/**
 * Keeps an active relay session running like a multiplayer client.
 *
 * The local world is still an integrated server, so vanilla would otherwise
 * pause it whenever a screen opens. Returning false for the integrated-server
 * checks used by the pause/game-menu paths suppresses that behavior without
 * stopping the integrated server itself.
 */
@Mixin(MinecraftClient.class)
public abstract class MinecraftClientMixin {
    @Redirect(
            method = "render",
            at = @At(value = "INVOKE", target = "Lnet/minecraft/client/MinecraftClient;isIntegratedServerRunning()Z"))
    private boolean smsrelay$doNotPauseRender(MinecraftClient client) {
        return smsrelay$isRelaySession() ? false : client.isIntegratedServerRunning();
    }

    @Redirect(
            method = "openGameMenu",
            at = @At(value = "INVOKE", target = "Lnet/minecraft/client/MinecraftClient;isIntegratedServerRunning()Z"))
    private boolean smsrelay$doNotPauseGameMenu(MinecraftClient client) {
        return smsrelay$isRelaySession() ? false : client.isIntegratedServerRunning();
    }

    private static boolean smsrelay$isRelaySession() {
        SmsRelayMod mod = SmsRelayMod.getInstance();
        return mod != null && mod.isSessionActive();
    }
}
