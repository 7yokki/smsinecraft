package dev.smsrelay.mixin;

import dev.smsrelay.SmsRelayMod;
import net.minecraft.block.BlockState;
import net.minecraft.client.MinecraftClient;
import net.minecraft.client.network.ClientPlayerEntity;
import net.minecraft.client.network.ClientPlayerInteractionManager;
import net.minecraft.entity.Entity;
import net.minecraft.entity.player.PlayerEntity;
import net.minecraft.util.ActionResult;
import net.minecraft.util.Hand;
import net.minecraft.util.hit.BlockHitResult;
import net.minecraft.util.math.BlockPos;
import org.spongepowered.asm.mixin.Final;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.Shadow;
import org.spongepowered.asm.mixin.Unique;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfoReturnable;

/**
 * Observes only local interaction methods. No vanilla behavior is replaced;
 * failures to apply remote changes are handled in WorldSync instead.
 */
@Mixin(ClientPlayerInteractionManager.class)
public abstract class ClientPlayerInteractionManagerMixin {
    @Shadow
    @Final
    private MinecraftClient client;

    @Unique
    private BlockPos smsrelay$hitPos;
    @Unique
    private BlockState smsrelay$hitBefore;
    @Unique
    private BlockPos smsrelay$targetPos;
    @Unique
    private BlockState smsrelay$targetBefore;

    @Inject(method = "breakBlock", at = @At("RETURN"))
    private void smsrelay$afterBreak(BlockPos pos, CallbackInfoReturnable<Boolean> cir) {
        if (!cir.getReturnValueZ() || client.world == null) {
            return;
        }
        SmsRelayMod mod = SmsRelayMod.getInstance();
        if (mod != null) {
            mod.onLocalBlockChange(pos, client.world.getBlockState(pos));
        }
    }

    @Inject(method = "interactBlock", at = @At("HEAD"))
    private void smsrelay$beforeInteract(
            ClientPlayerEntity player,
            Hand hand,
            BlockHitResult hitResult,
            CallbackInfoReturnable<ActionResult> cir) {
        if (client.world == null) {
            return;
        }
        smsrelay$hitPos = hitResult.getBlockPos();
        smsrelay$hitBefore = client.world.getBlockState(smsrelay$hitPos);
        smsrelay$targetPos = smsrelay$hitPos.offset(hitResult.getSide());
        smsrelay$targetBefore = client.world.getBlockState(smsrelay$targetPos);
    }

    @Inject(method = "interactBlock", at = @At("RETURN"))
    private void smsrelay$afterInteract(
            ClientPlayerEntity player,
            Hand hand,
            BlockHitResult hitResult,
            CallbackInfoReturnable<ActionResult> cir) {
        if (client.world == null || smsrelay$hitPos == null) {
            return;
        }
        SmsRelayMod mod = SmsRelayMod.getInstance();
        if (mod == null) {
            return;
        }
        if (smsrelay$hitBefore != client.world.getBlockState(smsrelay$hitPos)) {
            mod.onLocalBlockChange(smsrelay$hitPos, client.world.getBlockState(smsrelay$hitPos));
        }
        if (smsrelay$targetPos != null && smsrelay$targetBefore != client.world.getBlockState(smsrelay$targetPos)) {
            mod.onLocalBlockChange(smsrelay$targetPos, client.world.getBlockState(smsrelay$targetPos));
        }
        smsrelay$hitPos = null;
        smsrelay$hitBefore = null;
        smsrelay$targetPos = null;
        smsrelay$targetBefore = null;
    }

    @Inject(method = "attackEntity", at = @At("TAIL"))
    private void smsrelay$afterAttack(PlayerEntity player, Entity target, CallbackInfo ci) {
        if (client.player == null || player != client.player) {
            return;
        }
        SmsRelayMod mod = SmsRelayMod.getInstance();
        if (mod != null) {
            mod.onLocalAttack(target.getId(), target.getX(), target.getY() + target.getHeight() * 0.5, target.getZ());
        }
    }
}
