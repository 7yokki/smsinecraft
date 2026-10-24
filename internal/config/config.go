package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"
)

type Config struct {
	Addr string

	MaxFrameBytes        uint32
	HandshakeTimeout     time.Duration
	IdleTimeout          time.Duration
	WriteTimeout         time.Duration
	PingInterval         time.Duration
	OutboundQueueFrames  int
	OutboundQueueBytes   int64
	RateLimitPerSecond   float64
	RateLimitBurst       float64
	CleanupInterval      time.Duration
	ChunkOfflineTTL      time.Duration
	ChunkRequestTTL      time.Duration
	EntityTTL            time.Duration
	BlockLogSize         int
	MaxChunkSummaryBytes int
	MaxChunkAuthorities  int
	MaxReach             float64
	AttackCooldown       time.Duration
	Movement             MovementLimits
}

type MovementLimits struct {
	SurvivalMaxSpeed    float64
	CreativeMaxSpeed    float64
	SpectatorMaxSpeed   float64
	MaxVerticalSpeed    float64
	MaxGroundVertical   float64
	Tolerance           float64
	ViolationThreshold  int
	MinMovementInterval time.Duration
}

func Default() Config {
	return Config{
		Addr:                 ":25566",
		MaxFrameBytes:        2 << 20,
		HandshakeTimeout:     10 * time.Second,
		IdleTimeout:          90 * time.Second,
		WriteTimeout:         10 * time.Second,
		PingInterval:         30 * time.Second,
		OutboundQueueFrames:  64,
		OutboundQueueBytes:   4 << 20,
		RateLimitPerSecond:   120,
		RateLimitBurst:       240,
		CleanupInterval:      30 * time.Second,
		ChunkOfflineTTL:      5 * time.Minute,
		ChunkRequestTTL:      30 * time.Second,
		EntityTTL:            2 * time.Minute,
		BlockLogSize:         512,
		MaxChunkSummaryBytes: 2048,
		MaxChunkAuthorities:  16384,
		MaxReach:             6,
		AttackCooldown:       300 * time.Millisecond,
		Movement: MovementLimits{
			SurvivalMaxSpeed:    24,
			CreativeMaxSpeed:    120,
			SpectatorMaxSpeed:   240,
			MaxVerticalSpeed:    120,
			MaxGroundVertical:   1.5,
			Tolerance:           1.0,
			ViolationThreshold:  3,
			MinMovementInterval: time.Millisecond,
		},
	}
}

func (c Config) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("addr must not be empty")
	}
	if c.MaxFrameBytes < 64 || c.MaxFrameBytes > 64<<20 {
		return fmt.Errorf("max-frame must be between 64 bytes and 64 MiB")
	}
	if c.OutboundQueueFrames < 1 {
		return fmt.Errorf("outbound-queue-frames must be positive")
	}
	if c.OutboundQueueBytes < int64(c.MaxFrameBytes) {
		return fmt.Errorf("outbound-queue-bytes must be at least max-frame")
	}
	if c.RateLimitPerSecond <= 0 || c.RateLimitBurst <= 0 {
		return fmt.Errorf("rate limits must be positive")
	}
	if c.IdleTimeout <= 0 || c.PingInterval <= 0 || c.PingInterval >= c.IdleTimeout {
		return fmt.Errorf("ping-interval must be positive and lower than idle-timeout")
	}
	if c.BlockLogSize < 1 {
		return fmt.Errorf("block-log-size must be positive")
	}
	if c.MaxChunkSummaryBytes < 0 {
		return fmt.Errorf("max-chunk-summary-bytes must not be negative")
	}
	if c.MaxChunkAuthorities < 1 {
		return fmt.Errorf("max-chunk-authorities must be positive")
	}
	if c.MaxReach <= 0 {
		return fmt.Errorf("max-reach must be positive")
	}
	if c.AttackCooldown < 0 {
		return fmt.Errorf("attack-cooldown must not be negative")
	}
	m := c.Movement
	if m.SurvivalMaxSpeed <= 0 || m.CreativeMaxSpeed <= 0 || m.SpectatorMaxSpeed <= 0 {
		return fmt.Errorf("movement speeds must be positive")
	}
	if m.MaxVerticalSpeed <= 0 || m.MaxGroundVertical <= 0 {
		return fmt.Errorf("vertical movement limits must be positive")
	}
	if m.Tolerance < 0 || m.ViolationThreshold < 1 {
		return fmt.Errorf("invalid movement tolerance or violation threshold")
	}
	return nil
}

func BindFlags(fs *flag.FlagSet, output io.Writer) *Config {
	cfg := Default()
	fs.SetOutput(output)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "TCP listen address")
	fs.Func("max-frame", "maximum wire frame size in bytes", func(s string) error {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return err
		}
		cfg.MaxFrameBytes = uint32(n)
		return nil
	})
	fs.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", cfg.HandshakeTimeout, "time allowed to send the connect packet")
	fs.DurationVar(&cfg.IdleTimeout, "idle-timeout", cfg.IdleTimeout, "connection read timeout")
	fs.DurationVar(&cfg.WriteTimeout, "write-timeout", cfg.WriteTimeout, "socket write timeout")
	fs.DurationVar(&cfg.PingInterval, "ping-interval", cfg.PingInterval, "application keep-alive interval")
	fs.IntVar(&cfg.OutboundQueueFrames, "outbound-queue-frames", cfg.OutboundQueueFrames, "maximum queued frames per client")
	fs.Int64Var(&cfg.OutboundQueueBytes, "outbound-queue-bytes", cfg.OutboundQueueBytes, "maximum queued bytes per client")
	fs.Float64Var(&cfg.RateLimitPerSecond, "rate-limit", cfg.RateLimitPerSecond, "incoming packets per second per client")
	fs.Float64Var(&cfg.RateLimitBurst, "rate-burst", cfg.RateLimitBurst, "incoming packet burst per client")
	fs.DurationVar(&cfg.CleanupInterval, "cleanup-interval", cfg.CleanupInterval, "state expiry scan interval")
	fs.DurationVar(&cfg.ChunkOfflineTTL, "chunk-offline-ttl", cfg.ChunkOfflineTTL, "how long a chunk authority is remembered after its owner disconnects")
	fs.DurationVar(&cfg.ChunkRequestTTL, "chunk-request-ttl", cfg.ChunkRequestTTL, "lifetime of an unanswered chunk request")
	fs.DurationVar(&cfg.EntityTTL, "entity-ttl", cfg.EntityTTL, "entity reference expiry after last update")
	fs.IntVar(&cfg.BlockLogSize, "block-log-size", cfg.BlockLogSize, "number of recent block changes retained for late joiners")
	fs.IntVar(&cfg.MaxChunkSummaryBytes, "max-chunk-summary-bytes", cfg.MaxChunkSummaryBytes, "maximum stored chunk summary size")
	fs.IntVar(&cfg.MaxChunkAuthorities, "max-chunk-authorities", cfg.MaxChunkAuthorities, "maximum chunk authority entries kept in RAM")
	fs.Float64Var(&cfg.MaxReach, "max-reach", cfg.MaxReach, "maximum block/attack interaction distance")
	fs.DurationVar(&cfg.AttackCooldown, "attack-cooldown", cfg.AttackCooldown, "minimum interval between accepted attacks")
	fs.Float64Var(&cfg.Movement.SurvivalMaxSpeed, "max-speed-survival", cfg.Movement.SurvivalMaxSpeed, "survival movement limit (blocks/s)")
	fs.Float64Var(&cfg.Movement.CreativeMaxSpeed, "max-speed-creative", cfg.Movement.CreativeMaxSpeed, "creative movement limit (blocks/s)")
	fs.Float64Var(&cfg.Movement.SpectatorMaxSpeed, "max-speed-spectator", cfg.Movement.SpectatorMaxSpeed, "spectator movement limit (blocks/s)")
	fs.Float64Var(&cfg.Movement.MaxVerticalSpeed, "max-vertical-speed", cfg.Movement.MaxVerticalSpeed, "vertical movement limit in air (blocks/s)")
	fs.Float64Var(&cfg.Movement.MaxGroundVertical, "max-ground-vertical", cfg.Movement.MaxGroundVertical, "vertical change while both samples are on ground")
	fs.Float64Var(&cfg.Movement.Tolerance, "movement-tolerance", cfg.Movement.Tolerance, "distance allowance added to movement checks")
	fs.IntVar(&cfg.Movement.ViolationThreshold, "movement-violations", cfg.Movement.ViolationThreshold, "consecutive rejected moves before a correction packet")
	fs.DurationVar(&cfg.Movement.MinMovementInterval, "min-movement-interval", cfg.Movement.MinMovementInterval, "minimum dt used for movement speed calculations")
	return &cfg
}
