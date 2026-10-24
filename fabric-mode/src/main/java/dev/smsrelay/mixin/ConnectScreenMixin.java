package dev.smsrelay.mixin;

import dev.smsrelay.SmsRelayMod;
import dev.smsrelay.config.RelayConfig;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.gui.screen.Screen;
import net.minecraft.client.gui.screen.multiplayer.ConnectScreen;
import net.minecraft.client.network.CookieStorage;
import net.minecraft.client.network.ServerAddress;
import net.minecraft.client.network.ServerInfo;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;

/**
 * Replaces the vanilla remote multiplayer connection with a local integrated
 * world plus the custom TCP relay connection.
 *
 * The injection point is the public static entry used by both the multiplayer
 * screen and quick-play. Cancelling it prevents any vanilla login packet from
 * being sent. The local world is started on the render thread; relay sockets
 * are opened later from dedicated network threads.
 */
@Mixin(ConnectScreen.class)
public abstract class ConnectScreenMixin {
    @Inject(method = "connect", at = @At("HEAD"), cancellable = true)
    private static void smsrelay$replaceConnection(
            Screen parent,
            MinecraftClient client,
            ServerAddress address,
            ServerInfo info,
            boolean quickPlay,
            CookieStorage cookieStorage,
            CallbackInfo ci) {
        if (!RelayConfig.interceptAllMultiplayer()) {
            return;
        }
        SmsRelayMod mod = SmsRelayMod.getInstance();
        if (mod == null) {
            return;
        }
        ci.cancel();
        mod.beginRelaySession(client, parent == null ? null : parent);
    }
}
