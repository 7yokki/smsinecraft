package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"smsinecraft/internal/anticheat"
	"smsinecraft/internal/config"
	"smsinecraft/internal/protocol"
	"smsinecraft/internal/state"
)

type Server struct {
	cfg    config.Config
	logger *slog.Logger
	state  *state.Manager
	ac     *anticheat.Manager

	mu      sync.RWMutex
	clients map[string]*Client
	pending map[string]map[string]time.Time

	ln        net.Listener
	stop      chan struct{}
	closeOnce sync.Once
	connMu    sync.Mutex
	wg        sync.WaitGroup
	seq       atomic.Uint64
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:    cfg,
		logger: logger,
		state: state.NewManager(state.Options{
			BlockLogSize:         cfg.BlockLogSize,
			MaxChunkSummaryBytes: cfg.MaxChunkSummaryBytes,
			MaxChunkAuthorities:  cfg.MaxChunkAuthorities,
		}),
		ac:      newAntiCheat(cfg),
		clients: make(map[string]*Client),
		pending: make(map[string]map[string]time.Time),
		stop:    make(chan struct{}),
	}, nil
}

func newAntiCheat(cfg config.Config) *anticheat.Manager {
	return anticheat.New(anticheat.Options{
		SurvivalMaxSpeed:    cfg.Movement.SurvivalMaxSpeed,
		CreativeMaxSpeed:    cfg.Movement.CreativeMaxSpeed,
		SpectatorMaxSpeed:   cfg.Movement.SpectatorMaxSpeed,
		MaxVerticalSpeed:    cfg.Movement.MaxVerticalSpeed,
		MaxGroundVertical:   cfg.Movement.MaxGroundVertical,
		Tolerance:           cfg.Movement.Tolerance,
		ViolationThreshold:  cfg.Movement.ViolationThreshold,
		MinMovementInterval: cfg.Movement.MinMovementInterval,
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.logger.Info("relay listening", "addr", ln.Addr().String())
	return s.Serve(ctx, ln)
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	defer s.Shutdown()

	go s.cleanupLoop()
	go func() {
		select {
		case <-ctx.Done():
			s.Shutdown()
		case <-s.stop:
		}
	}()

	var retryDelay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if retryDelay == 0 {
					retryDelay = 5 * time.Millisecond
				} else {
					retryDelay *= 2
				}
				if retryDelay > time.Second {
					retryDelay = time.Second
				}
				s.logger.Warn("temporary accept error", "err", err, "retry_in", retryDelay)
				select {
				case <-time.After(retryDelay):
					continue
				case <-s.stop:
					return nil
				}
			}
			return err
		}
		retryDelay = 0

		s.connMu.Lock()
		select {
		case <-s.stop:
			s.connMu.Unlock()
			_ = conn.Close()
			return nil
		default:
		}
		s.wg.Add(1)
		s.connMu.Unlock()

		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) Shutdown() {
	s.closeOnce.Do(func() {
		close(s.stop)

		s.mu.Lock()
		ln := s.ln
		clients := make([]*Client, 0, len(s.clients))
		for _, c := range s.clients {
			clients = append(clients, c)
		}
		s.mu.Unlock()

		if ln != nil {
			_ = ln.Close()
		}
		for _, c := range clients {
			c.close()
		}

		s.connMu.Lock()
		s.connMu.Unlock()
		s.wg.Wait()
	})
}

func (s *Server) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(s.cfg.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			stats := s.state.Cleanup(now, s.cfg.ChunkOfflineTTL, s.cfg.EntityTTL)
			expired := s.expirePending(now)
			if stats.Chunks > 0 || stats.Entities > 0 || expired > 0 {
				players, chunks, entities := s.state.Stats()
				s.logger.Info("state cleanup",
					"expired_chunks", stats.Chunks,
					"expired_entities", stats.Entities,
					"expired_requests", expired,
					"players", players,
					"chunk_authorities", chunks,
					"entity_refs", entities,
				)
			}
		case <-s.stop:
			return
		}
	}
}

func (s *Server) newFrame(channel string, payload []byte) protocol.Frame {
	return protocol.Frame{
		Channel:   channel,
		Sequence:  s.seq.Add(1),
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
	}
}

func (s *Server) broadcast(channel string, payload []byte, exceptID string) {
	frame := s.newFrame(channel, payload)

	s.mu.RLock()
	targets := make([]*Client, 0, len(s.clients))
	for id, c := range s.clients {
		if id != exceptID {
			targets = append(targets, c)
		}
	}
	s.mu.RUnlock()

	for _, c := range targets {
		if !c.enqueue(frame) {
			s.logger.Warn("closing slow client", "player", c.uuid, "remote", c.conn.RemoteAddr())
		}
	}
}

func (s *Server) send(c *Client, channel string, payload []byte) bool {
	if !c.enqueue(s.newFrame(channel, payload)) {
		s.logger.Warn("closing slow client", "player", c.uuid, "remote", c.conn.RemoteAddr())
		return false
	}
	return true
}

func (s *Server) sendError(c *Client, channel, code, message string) {
	payload, err := marshalJSON(protocol.ErrorPayload{Code: code, Message: message, Channel: channel})
	if err != nil {
		return
	}
	s.send(c, protocol.ChannelError, payload)
}

func (s *Server) broadcastPlayerList() {
	if s.stopping() {
		return
	}
	players := s.state.Players()
	sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
	infos := make([]protocol.PlayerInfo, 0, len(players))
	for _, p := range players {
		infos = append(infos, protocol.PlayerInfo{
			ID:       p.ID,
			Username: p.Username,
			GameMode: p.GameMode,
			World:    p.World,
			PingMS:   p.Ping.Milliseconds(),
		})
	}
	payload, err := marshalJSON(protocol.PlayerListPayload{
		Players:    infos,
		ServerTime: time.Now().UnixMilli(),
	})
	if err != nil {
		s.logger.Error("encode player list", "err", err)
		return
	}
	s.broadcast(protocol.ChannelPlayerList, payload, "")
}

func (s *Server) clientByID(id string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[id]
}

func (s *Server) isOnline(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.clients[id]
	return ok
}

func (s *Server) expirePending(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	expired := 0
	for chunkID, requesters := range s.pending {
		for requesterID, requestedAt := range requesters {
			if now.Sub(requestedAt) > s.cfg.ChunkRequestTTL {
				delete(requesters, requesterID)
				expired++
			}
		}
		if len(requesters) == 0 {
			delete(s.pending, chunkID)
		}
	}
	return expired
}

var errDuplicateClient = errors.New("player uuid already connected")

func (s *Server) register(c *Client, meta protocol.ConnectPayload, now time.Time) error {
	ackPayload, err := marshalJSON(protocol.ConnectAckPayload{
		PlayerID:       meta.UUID,
		Username:       meta.Username,
		ServerTime:     now.UnixMilli(),
		PingIntervalMS: s.cfg.PingInterval.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("encode connect ack: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping() {
		return errors.New("server shutting down")
	}
	if _, exists := s.clients[meta.UUID]; exists {
		return errDuplicateClient
	}
	if _, ok := s.state.AddPlayer(meta.UUID, meta.Username, meta.GameMode, meta.World, now); !ok {
		return errDuplicateClient
	}

	c.uuid = meta.UUID
	c.username = meta.Username
	c.rate = anticheat.NewRateLimiter(s.cfg.RateLimitPerSecond, s.cfg.RateLimitBurst, now)
	c.startWriter()
	c.enqueue(s.newFrame(protocol.ChannelConnectAck, ackPayload))
	s.clients[meta.UUID] = c
	return nil
}

func (s *Server) handleConn(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	c := newClient(s, conn)
	defer func() {
		s.unregister(c)
		c.close()
	}()

	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	frame, err := protocol.ReadFrame(conn, s.cfg.MaxFrameBytes)
	if err != nil {
		s.logger.Debug("handshake read failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	if frame.Channel != protocol.ChannelConnect {
		s.writeDirectError(conn, frame.Channel, "connect_required", "first packet must be sms:connect")
		return
	}

	var meta protocol.ConnectPayload
	if err := unmarshalJSON(frame.Payload, &meta); err != nil {
		s.writeDirectError(conn, frame.Channel, "invalid_connect", "malformed connect payload")
		return
	}
	if err := normalizeAndValidateConnect(&meta); err != nil {
		s.writeDirectError(conn, frame.Channel, "invalid_connect", err.Error())
		return
	}

	if err := s.register(c, meta, time.Now()); err != nil {
		code := "server_error"
		if errors.Is(err, errDuplicateClient) {
			code = "duplicate_uuid"
		}
		s.writeDirectError(conn, frame.Channel, code, err.Error())
		return
	}

	s.finishRegistration(c)
	go c.heartbeatLoop()

	s.logger.Info("player connected", "player", c.uuid, "username", c.username, "remote", conn.RemoteAddr())
	s.readLoop(c)
}

func (s *Server) finishRegistration(c *Client) {
	s.sendBlockLog(c)
	s.sendWorldState(c)
	s.broadcastPlayerList()
}

func (s *Server) readLoop(c *Client) {
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		frame, err := protocol.ReadFrame(c.conn, s.cfg.MaxFrameBytes)
		if err != nil {
			if !s.stopping() {
				s.logger.Debug("connection read ended", "player", c.uuid, "err", err)
			}
			return
		}
		now := time.Now()
		s.state.Touch(c.uuid, now)

		switch frame.Channel {
		case protocol.ChannelPong:
			s.handlePong(c, frame, now)
			continue
		case protocol.ChannelDisconnect:
			var p protocol.DisconnectPayload
			_ = unmarshalJSON(frame.Payload, &p)
			s.logger.Info("player disconnected", "player", c.uuid, "reason", p.Reason)
			return
		}

		if !c.rate.Allow(now) {
			c.warnRateLimited(now, s)
			continue
		}
		if err := s.handleFrame(c, frame, now); err != nil {
			s.logger.Warn("packet handler error", "player", c.uuid, "channel", frame.Channel, "err", err)
		}
	}
}

func (s *Server) unregister(c *Client) {
	if c.uuid == "" {
		return
	}

	now := time.Now()
	s.mu.Lock()
	registered := s.clients[c.uuid] == c
	if registered {
		delete(s.clients, c.uuid)
	}
	var pendingRequesters []*Client
	if registered {
		s.state.RemovePlayer(c.uuid, now)
		for chunkID, requesters := range s.pending {
			chunk, ok := s.state.GetChunk(chunkID)
			if !ok || chunk.OwnerID != c.uuid {
				continue
			}
			delete(s.pending, chunkID)
			for requesterID := range requesters {
				if rc := s.clients[requesterID]; rc != nil {
					pendingRequesters = append(pendingRequesters, rc)
				}
			}
		}
	}
	s.mu.Unlock()

	s.ac.Remove(c.uuid)
	if registered {
		for _, rc := range pendingRequesters {
			s.sendError(rc, protocol.ChannelChunkRequest, "authority_offline", "chunk authority disconnected before responding")
		}
		if !s.stopping() {
			s.broadcastPlayerList()
		}
		s.logger.Info("player removed", "player", c.uuid, "username", c.username)
	}
}

func (s *Server) writeDirectError(conn net.Conn, channel, code, message string) {
	payload, err := marshalJSON(protocol.ErrorPayload{Code: code, Message: message, Channel: channel})
	if err != nil {
		return
	}
	frame := s.newFrame(protocol.ChannelError, payload)
	_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	_ = protocol.WriteFrame(conn, frame)
}
