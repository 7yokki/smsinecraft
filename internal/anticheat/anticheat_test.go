package anticheat

import (
	"testing"
	"time"

	"smsinecraft/internal/state"
)

func testManager(threshold int) *Manager {
	return New(Options{
		SurvivalMaxSpeed:    10,
		CreativeMaxSpeed:    100,
		SpectatorMaxSpeed:   200,
		MaxVerticalSpeed:    50,
		MaxGroundVertical:   1.5,
		Tolerance:           0.5,
		ViolationThreshold:  threshold,
		MinMovementInterval: time.Millisecond,
	})
}

func TestMovementWithinLimit(t *testing.T) {
	m := testManager(3)
	now := time.Unix(100, 0)
	first := m.CheckMovement("p", "survival", state.Vec3{}, true, now)
	if !first.Allowed {
		t.Fatal("first sample must be accepted")
	}
	second := m.CheckMovement("p", "survival", state.Vec3{X: 1}, true, now.Add(200*time.Millisecond))
	if !second.Allowed {
		t.Fatalf("normal move rejected: %+v", second)
	}
}

func TestMovementTeleportCorrection(t *testing.T) {
	m := testManager(2)
	now := time.Unix(100, 0)
	m.CheckMovement("p", "survival", state.Vec3{}, true, now)

	first := m.CheckMovement("p", "survival", state.Vec3{X: 100}, false, now.Add(time.Millisecond))
	if first.Allowed || first.Correct {
		t.Fatalf("expected rejected move without correction yet: %+v", first)
	}
	second := m.CheckMovement("p", "survival", state.Vec3{X: 200}, false, now.Add(2*time.Millisecond))
	if second.Allowed || !second.Correct {
		t.Fatalf("expected correction on second violation: %+v", second)
	}
}

func TestGroundedVerticalJump(t *testing.T) {
	m := testManager(1)
	now := time.Unix(100, 0)
	m.CheckMovement("p", "survival", state.Vec3{Y: 64}, true, now)

	res := m.CheckMovement("p", "survival", state.Vec3{Y: 70}, true, now.Add(50*time.Millisecond))
	if res.Allowed {
		t.Fatalf("grounded vertical jump should be rejected: %+v", res)
	}

	res = m.CheckMovement("p", "survival", state.Vec3{Y: 65}, false, now.Add(100*time.Millisecond))
	if !res.Allowed {
		t.Fatalf("transition to airborne should be allowed: %+v", res)
	}
}

func TestAttackCooldown(t *testing.T) {
	m := testManager(1)
	now := time.Unix(100, 0)
	ok, _ := m.CheckAttack("p", 300*time.Millisecond, now)
	if !ok {
		t.Fatal("first attack should be allowed")
	}
	ok, remaining := m.CheckAttack("p", 300*time.Millisecond, now.Add(100*time.Millisecond))
	if ok || remaining <= 0 {
		t.Fatalf("second attack should be on cooldown: ok=%v remaining=%v", ok, remaining)
	}
	ok, _ = m.CheckAttack("p", 300*time.Millisecond, now.Add(301*time.Millisecond))
	if !ok {
		t.Fatal("attack after cooldown should be allowed")
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(100, 0)
	l := NewRateLimiter(2, 2, now)
	if !l.Allow(now) || !l.Allow(now) {
		t.Fatal("burst should be allowed")
	}
	if l.Allow(now) {
		t.Fatal("third immediate packet should be rejected")
	}
	if !l.Allow(now.Add(500 * time.Millisecond)) {
		t.Fatal("token should refill after 500ms")
	}
}
