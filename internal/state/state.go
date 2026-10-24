package state

import (
	"errors"
	"math"
	"sync"
	"time"
)

type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func (v Vec3) Distance(other Vec3) float64 {
	dx := v.X - other.X
	dy := v.Y - other.Y
	dz := v.Z - other.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func (v Vec3) Valid() bool {
	return isFinite(v.X) && isFinite(v.Y) && isFinite(v.Z)
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

type Player struct {
	ID           string
	Username     string
	GameMode     string
	World        string
	Position     Vec3
	Yaw          float64
	Pitch        float64
	OnGround     bool
	LastSeen     time.Time
	LastMovement time.Time
	Ping         time.Duration
}

type Entity struct {
	ID            string
	Type          string
	Position      Vec3
	AuthorityID   string
	AuthorityName string
	LastUpdate    time.Time
}

type ChunkAuthority struct {
	ChunkID   string
	OwnerID   string
	OwnerName string
	Summary   []byte
	Revision  uint64
	UpdatedAt time.Time
	Online    bool
	OfflineAt time.Time
}

type BlockEvent struct {
	Sequence   uint64
	ActorID    string
	ActorName  string
	World      string
	X          int
	Y          int
	Z          int
	BlockID    string
	ServerTime int64
}

type WorldState struct {
	TimeOfDay      int64   `json:"time_of_day"`
	Weather        string  `json:"weather"`
	SpawnX         float64 `json:"spawn_x"`
	SpawnY         float64 `json:"spawn_y"`
	SpawnZ         float64 `json:"spawn_z"`
	BorderDiameter float64 `json:"border_diameter"`
	UpdatedAt      int64   `json:"updated_at"`
}

type Options struct {
	BlockLogSize         int
	MaxChunkSummaryBytes int
	MaxChunkAuthorities  int
}

type Manager struct {
	mu sync.RWMutex

	opts     Options
	players  map[string]*Player
	entities map[string]*Entity
	chunks   map[string]*ChunkAuthority

	world WorldState

	blockEvents []BlockEvent
	blockNext   int
	blockSeq    uint64
}

var (
	ErrChunkSummaryTooLarge = errors.New("chunk summary too large")
	ErrChunkOwned           = errors.New("chunk owned by another online client")
)

type ChunkClaimResult int

const (
	ChunkClaimed ChunkClaimResult = iota
	ChunkUpdated
	ChunkTakenOver
	ChunkConflict
	ChunkRejected
)

func NewManager(opts Options) *Manager {
	if opts.BlockLogSize < 1 {
		opts.BlockLogSize = 1
	}
	if opts.MaxChunkAuthorities < 1 {
		opts.MaxChunkAuthorities = 1
	}
	return &Manager{
		opts:     opts,
		players:  make(map[string]*Player),
		entities: make(map[string]*Entity),
		chunks:   make(map[string]*ChunkAuthority),
		world: WorldState{
			TimeOfDay:      6000,
			Weather:        "clear",
			BorderDiameter: 1000,
		},
	}
}

func (m *Manager) AddPlayer(id, username, gameMode, world string, now time.Time) (Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[id]; ok {
		return *p, false
	}
	p := &Player{
		ID:       id,
		Username: username,
		GameMode: gameMode,
		World:    world,
		LastSeen: now,
	}
	m.players[id] = p

	for _, c := range m.chunks {
		if c.OwnerID == id {
			c.Online = true
			c.OfflineAt = time.Time{}
		}
	}
	return *p, true
}

func (m *Manager) RemovePlayer(id string, now time.Time) []Entity {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.players, id)

	for _, c := range m.chunks {
		if c.OwnerID == id {
			c.Online = false
			c.OfflineAt = now
		}
	}

	var released []Entity
	for _, e := range m.entities {
		if e.AuthorityID == id {
			e.AuthorityID = ""
			e.AuthorityName = ""
			e.LastUpdate = now
			released = append(released, *e)
		}
	}
	return released
}

func (m *Manager) GetPlayer(id string) (Player, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.players[id]
	if !ok {
		return Player{}, false
	}
	return *p, true
}

func (m *Manager) Players() []Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Player, 0, len(m.players))
	for _, p := range m.players {
		out = append(out, *p)
	}
	return out
}

func (m *Manager) UpdatePosition(id string, pos Vec3, yaw, pitch float64, onGround bool, now time.Time) (Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[id]
	if !ok {
		return Player{}, false
	}
	p.Position = pos
	p.Yaw = yaw
	p.Pitch = pitch
	p.OnGround = onGround
	p.LastSeen = now
	p.LastMovement = now
	return *p, true
}

func (m *Manager) Touch(id string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[id]; ok {
		p.LastSeen = now
	}
}

func (m *Manager) SetPing(id string, ping time.Duration, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[id]; ok {
		p.Ping = ping
		p.LastSeen = now
	}
}

func (m *Manager) AppendBlockEvent(e BlockEvent) BlockEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockSeq++
	e.Sequence = m.blockSeq

	if len(m.blockEvents) < m.opts.BlockLogSize {
		m.blockEvents = append(m.blockEvents, e)
		return e
	}
	m.blockEvents[m.blockNext] = e
	m.blockNext = (m.blockNext + 1) % m.opts.BlockLogSize
	return e
}

func (m *Manager) BlockEvents() []BlockEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.blockEvents) < m.opts.BlockLogSize {
		out := make([]BlockEvent, len(m.blockEvents))
		copy(out, m.blockEvents)
		return out
	}
	out := make([]BlockEvent, 0, len(m.blockEvents))
	out = append(out, m.blockEvents[m.blockNext:]...)
	out = append(out, m.blockEvents[:m.blockNext]...)
	return out
}

func (m *Manager) ClaimChunk(chunkID, ownerID, ownerName string, summary []byte, now time.Time) (ChunkAuthority, ChunkClaimResult, error) {
	if len(summary) > m.opts.MaxChunkSummaryBytes {
		return ChunkAuthority{}, ChunkRejected, ErrChunkSummaryTooLarge
	}
	copied := append([]byte(nil), summary...)

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.chunks[chunkID]
	if !ok {
		if len(m.chunks) >= m.opts.MaxChunkAuthorities {
			m.evictChunk()
		}
		c := &ChunkAuthority{
			ChunkID:   chunkID,
			OwnerID:   ownerID,
			OwnerName: ownerName,
			Summary:   copied,
			Revision:  1,
			UpdatedAt: now,
			Online:    true,
		}
		m.chunks[chunkID] = c
		return *c, ChunkClaimed, nil
	}

	if existing.OwnerID == ownerID {
		existing.OwnerName = ownerName
		if summary != nil {
			existing.Summary = copied
		}
		existing.Revision++
		existing.UpdatedAt = now
		existing.Online = true
		existing.OfflineAt = time.Time{}
		return *existing, ChunkUpdated, nil
	}

	if existing.Online {
		return *existing, ChunkConflict, ErrChunkOwned
	}

	existing.OwnerID = ownerID
	existing.OwnerName = ownerName
	existing.Summary = copied
	existing.Revision++
	existing.UpdatedAt = now
	existing.Online = true
	existing.OfflineAt = time.Time{}
	return *existing, ChunkTakenOver, nil
}

func (m *Manager) evictChunk() {
	var victim string
	var victimAt time.Time
	for id, c := range m.chunks {
		if c.Online {
			continue
		}
		if victim == "" || c.UpdatedAt.Before(victimAt) {
			victim = id
			victimAt = c.UpdatedAt
		}
	}
	if victim == "" {
		for id, c := range m.chunks {
			if victim == "" || c.UpdatedAt.Before(victimAt) {
				victim = id
				victimAt = c.UpdatedAt
			}
		}
	}
	delete(m.chunks, victim)
}

func (m *Manager) GetChunk(chunkID string) (ChunkAuthority, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.chunks[chunkID]
	if !ok {
		return ChunkAuthority{}, false
	}
	return *c, true
}

func (m *Manager) UpsertEntity(id, typ string, pos Vec3, authorityID, authorityName string, now time.Time) Entity {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entities[id]
	if !ok {
		e = &Entity{ID: id}
		m.entities[id] = e
	}
	e.Type = typ
	e.Position = pos
	e.AuthorityID = authorityID
	e.AuthorityName = authorityName
	e.LastUpdate = now
	return *e
}

func (m *Manager) GetEntity(id string) (Entity, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entities[id]
	if !ok {
		return Entity{}, false
	}
	return *e, true
}

func (m *Manager) World() WorldState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.world
}

func (m *Manager) SetWorld(w WorldState, now time.Time) WorldState {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.UpdatedAt = now.UnixMilli()
	m.world = w
	return m.world
}

type CleanupStats struct {
	Chunks   int
	Entities int
}

func (m *Manager) Cleanup(now time.Time, chunkTTL, entityTTL time.Duration) CleanupStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stats CleanupStats

	for id, c := range m.chunks {
		if !c.Online && !c.OfflineAt.IsZero() && now.Sub(c.OfflineAt) > chunkTTL {
			delete(m.chunks, id)
			stats.Chunks++
		}
	}
	for id, e := range m.entities {
		if now.Sub(e.LastUpdate) > entityTTL {
			delete(m.entities, id)
			stats.Entities++
		}
	}
	return stats
}

func (m *Manager) Stats() (players, chunks, entities int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.players), len(m.chunks), len(m.entities)
}
