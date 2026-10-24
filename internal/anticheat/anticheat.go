package anticheat

import (
	"math"
	"sync"
	"time"

	"smsinecraft/internal/state"
)

type Options struct {
	SurvivalMaxSpeed    float64
	CreativeMaxSpeed    float64
	SpectatorMaxSpeed   float64
	MaxVerticalSpeed    float64
	MaxGroundVertical   float64
	Tolerance           float64
	ViolationThreshold  int
	MinMovementInterval time.Duration
}

type Manager struct {
	mu      sync.Mutex
	opts    Options
	players map[string]*playerState
}

type playerState struct {
	initialized bool
	lastPos     state.Vec3
	lastAt      time.Time
	lastGround  bool
	violations  int
	lastAttack  time.Time
	hasAttack   bool
}

type MovementResult struct {
	Allowed bool
	Correct bool
	Reason  string
	Speed   float64
}

func New(opts Options) *Manager {
	if opts.ViolationThreshold < 1 {
		opts.ViolationThreshold = 1
	}
	if opts.MinMovementInterval <= 0 {
		opts.MinMovementInterval = time.Millisecond
	}
	return &Manager{opts: opts, players: make(map[string]*playerState)}
}

func (m *Manager) Remove(playerID string) {
	m.mu.Lock()
	delete(m.players, playerID)
	m.mu.Unlock()
}

func (m *Manager) CheckMovement(playerID, gameMode string, pos state.Vec3, onGround bool, now time.Time) MovementResult {
	if !pos.Valid() {
		return MovementResult{Allowed: false, Reason: "non-finite position"}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.players[playerID]
	if st == nil {
		st = &playerState{}
		m.players[playerID] = st
	}
	if !st.initialized {
		st.initialized = true
		st.lastPos = pos
		st.lastAt = now
		st.lastGround = onGround
		return MovementResult{Allowed: true}
	}

	dt := now.Sub(st.lastAt).Seconds()
	if dt < m.opts.MinMovementInterval.Seconds() {
		dt = m.opts.MinMovementInterval.Seconds()
	}

	dx := pos.X - st.lastPos.X
	dy := pos.Y - st.lastPos.Y
	dz := pos.Z - st.lastPos.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	speed := dist / dt

	reason := ""
	maxSpeed := m.maxSpeed(gameMode)
	if dist > maxSpeed*dt+m.opts.Tolerance {
		reason = "movement speed"
	}

	if reason == "" {
		if st.lastGround && onGround {
			if math.Abs(dy) > m.opts.MaxGroundVertical+m.opts.Tolerance {
				reason = "vertical movement while grounded"
			}
		} else if math.Abs(dy) > m.opts.MaxVerticalSpeed*dt+m.opts.Tolerance {
			reason = "vertical speed"
		}
	}

	if reason != "" {
		st.violations++
		st.lastAt = now
		correct := st.violations >= m.opts.ViolationThreshold
		if correct {
			st.violations = 0
		}
		return MovementResult{Allowed: false, Correct: correct, Reason: reason, Speed: speed}
	}

	st.lastPos = pos
	st.lastAt = now
	st.lastGround = onGround
	st.violations = 0
	return MovementResult{Allowed: true, Speed: speed}
}

func (m *Manager) maxSpeed(mode string) float64 {
	switch mode {
	case "creative":
		return m.opts.CreativeMaxSpeed
	case "spectator":
		return m.opts.SpectatorMaxSpeed
	default:
		return m.opts.SurvivalMaxSpeed
	}
}

func (m *Manager) CheckAttack(playerID string, cooldown time.Duration, now time.Time) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.players[playerID]
	if st == nil {
		st = &playerState{}
		m.players[playerID] = st
	}
	if st.hasAttack {
		remaining := cooldown - now.Sub(st.lastAttack)
		if remaining > 0 {
			return false, remaining
		}
	}
	st.lastAttack = now
	st.hasAttack = true
	return true, 0
}

func WithinReach(from, target state.Vec3, reach float64) bool {
	return from.Distance(target) <= reach
}

type RateLimiter struct {
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func NewRateLimiter(rate, burst float64, now time.Time) *RateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{rate: rate, burst: burst, tokens: burst, last: now}
}

func (l *RateLimiter) Allow(now time.Time) bool {
	elapsed := now.Sub(l.last).Seconds()
	if elapsed > 0 {
		l.tokens = math.Min(l.burst, l.tokens+elapsed*l.rate)
		l.last = now
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
