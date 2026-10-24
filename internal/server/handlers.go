package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"smsinecraft/internal/protocol"
	"smsinecraft/internal/state"
)

const (
	maxCoordinate = 30_000_000
	maxEntityID   = 128
	maxBlockID    = 128
)

func (s *Server) handleFrame(c *Client, frame protocol.Frame, now time.Time) error {
	switch frame.Channel {
	case protocol.ChannelConnect:
		s.sendError(c, frame.Channel, "already_connected", "connect may only be sent once")
	case protocol.ChannelPosition:
		s.handlePosition(c, frame, now)
	case protocol.ChannelBlockChange:
		s.handleBlockChange(c, frame, now)
	case protocol.ChannelChunkProduced:
		s.handleChunkProduced(c, frame, now)
	case protocol.ChannelChunkRequest:
		s.handleChunkRequest(c, frame, now)
	case protocol.ChannelChunkResponse:
		s.handleChunkResponse(c, frame, now)
	case protocol.ChannelEntityUpdate:
		s.handleEntityUpdate(c, frame, now)
	case protocol.ChannelAttack:
		s.handleAttack(c, frame, now)
	case protocol.ChannelWorldState:
		s.handleWorldState(c, frame, now)
	case protocol.ChannelPing:
		s.handlePing(c, frame, now)
	case protocol.ChannelPong:
		s.handlePong(c, frame, now)
	default:
		s.sendError(c, frame.Channel, "unknown_channel", "unsupported channel")
	}
	return nil
}

func (s *Server) handlePosition(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.PositionUpdatePayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed position payload")
		return
	}
	pos := state.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	if !validPosition(pos) || !finite(p.Yaw) || !finite(p.Pitch) {
		s.sendError(c, frame.Channel, "invalid_position", "position or rotation is out of range")
		return
	}

	player, ok := s.state.GetPlayer(c.uuid)
	if !ok {
		return
	}
	result := s.ac.CheckMovement(c.uuid, player.GameMode, pos, p.OnGround, now)
	if !result.Allowed {
		if result.Correct {
			payload, err := marshalJSON(protocol.CorrectionPayload{
				X:          player.Position.X,
				Y:          player.Position.Y,
				Z:          player.Position.Z,
				Yaw:        player.Yaw,
				Pitch:      player.Pitch,
				Reason:     result.Reason,
				ServerTime: now.UnixMilli(),
			})
			if err == nil {
				s.send(c, protocol.ChannelCorrection, payload)
			}
		}
		return
	}

	updated, ok := s.state.UpdatePosition(c.uuid, pos, p.Yaw, p.Pitch, p.OnGround, now)
	if !ok {
		return
	}
	payload, err := marshalJSON(protocol.PositionStatePayload{
		PlayerID:   updated.ID,
		Username:   updated.Username,
		X:          updated.Position.X,
		Y:          updated.Position.Y,
		Z:          updated.Position.Z,
		Yaw:        updated.Yaw,
		Pitch:      updated.Pitch,
		OnGround:   updated.OnGround,
		ServerTime: now.UnixMilli(),
	})
	if err != nil {
		return
	}
	s.broadcast(protocol.ChannelPosition, payload, c.uuid)
}

func (s *Server) handleBlockChange(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.BlockChangePayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed block change payload")
		return
	}
	blockID, err := normalizeIdentifier(p.BlockID, maxBlockID)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_block", err.Error())
		return
	}
	p.BlockID = blockID

	player, ok := s.state.GetPlayer(c.uuid)
	if !ok {
		return
	}
	if player.LastMovement.IsZero() {
		s.sendError(c, frame.Channel, "position_required", "send a position update before interacting")
		return
	}
	if p.World == "" {
		p.World = player.World
	} else if p.World != player.World {
		s.sendError(c, frame.Channel, "world_mismatch", "block change is not in the player's world")
		return
	}
	if outOfRange(p.X) || outOfRange(p.Y) || outOfRange(p.Z) {
		s.sendError(c, frame.Channel, "invalid_block", "block coordinate is out of range")
		return
	}

	target := state.Vec3{X: float64(p.X) + 0.5, Y: float64(p.Y) + 0.5, Z: float64(p.Z) + 0.5}
	if !withinReach(player.Position, target, s.cfg.MaxReach) {
		s.sendError(c, frame.Channel, "reach", fmt.Sprintf("block is farther than %.1f blocks", s.cfg.MaxReach))
		return
	}

	event := s.state.AppendBlockEvent(state.BlockEvent{
		ActorID:    player.ID,
		ActorName:  player.Username,
		World:      p.World,
		X:          p.X,
		Y:          p.Y,
		Z:          p.Z,
		BlockID:    p.BlockID,
		ServerTime: now.UnixMilli(),
	})
	payload, err := marshalJSON(blockEventPayload(event))
	if err != nil {
		return
	}
	s.broadcast(protocol.ChannelBlockChange, payload, c.uuid)
}

func (s *Server) handleChunkProduced(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.ChunkProducedPayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed chunk_produced payload")
		return
	}
	chunkID, err := normalizeIdentifier(p.ChunkID, 128)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_chunk", err.Error())
		return
	}
	p.ChunkID = chunkID
	if len(p.Summary) > s.cfg.MaxChunkSummaryBytes {
		s.sendError(c, frame.Channel, "invalid_chunk", "chunk summary is too large")
		return
	}

	chunk, _, err := s.state.ClaimChunk(chunkID, c.uuid, c.username, p.Summary, now)
	status := protocol.ChunkStatusAccepted
	if err != nil {
		switch {
		case errors.Is(err, state.ErrChunkOwned):
			status = protocol.ChunkStatusConflict
		case errors.Is(err, state.ErrChunkSummaryTooLarge):
			s.sendError(c, frame.Channel, "invalid_chunk", err.Error())
			return
		default:
			s.sendError(c, frame.Channel, "chunk_error", err.Error())
			return
		}
	}
	payload, err := marshalJSON(protocol.ChunkAuthorityPayload{
		ChunkID:    chunk.ChunkID,
		OwnerID:    chunk.OwnerID,
		OwnerName:  chunk.OwnerName,
		Summary:    chunk.Summary,
		Status:     status,
		ServerTime: now.UnixMilli(),
	})
	if err != nil {
		return
	}
	s.send(c, protocol.ChannelChunkAuthority, payload)
}

func (s *Server) handleChunkRequest(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.ChunkRequestPayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed chunk_request payload")
		return
	}
	chunkID, err := normalizeIdentifier(p.ChunkID, 128)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_chunk", err.Error())
		return
	}

	chunk, ok := s.state.GetChunk(chunkID)
	if !ok {
		s.sendError(c, frame.Channel, "chunk_not_found", "no authority has produced this chunk yet")
		return
	}
	if chunk.OwnerID == c.uuid {
		s.sendError(c, frame.Channel, "chunk_self", "you are already the authority for this chunk")
		return
	}
	owner := s.clientByID(chunk.OwnerID)
	if owner == nil || !chunk.Online {
		s.sendError(c, frame.Channel, "authority_offline", "the chunk authority is not connected")
		return
	}

	s.addPending(chunkID, c.uuid, now)
	payload, err := marshalJSON(protocol.ChunkRequestRelayPayload{
		ChunkID:       chunkID,
		RequesterID:   c.uuid,
		RequesterName: c.username,
		ServerTime:    now.UnixMilli(),
	})
	if err != nil {
		s.dropPending(chunkID, c.uuid)
		return
	}
	if !s.send(owner, protocol.ChannelChunkRequest, payload) {
		s.dropPending(chunkID, c.uuid)
		s.sendError(c, frame.Channel, "authority_offline", "chunk authority disconnected before receiving the request")
	}
}

func (s *Server) handleChunkResponse(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.ChunkResponsePayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed chunk_response payload")
		return
	}
	chunkID, err := normalizeIdentifier(p.ChunkID, 128)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_chunk", err.Error())
		return
	}

	chunk, ok := s.state.GetChunk(chunkID)
	if !ok || chunk.OwnerID != c.uuid {
		s.sendError(c, frame.Channel, "not_authority", "you are not the authority for this chunk")
		return
	}

	requesters := s.takePending(chunkID)
	if len(requesters) == 0 {
		return
	}
	payload, err := marshalJSON(protocol.ChunkResponseRelayPayload{
		ChunkID:    chunkID,
		Data:       p.Data,
		FromID:     c.uuid,
		FromName:   c.username,
		ServerTime: now.UnixMilli(),
	})
	if err != nil {
		return
	}
	for requesterID := range requesters {
		if requester := s.clientByID(requesterID); requester != nil {
			s.send(requester, protocol.ChannelChunkResponse, payload)
		}
	}
}

func (s *Server) handleEntityUpdate(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.EntityUpdatePayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed entity_update payload")
		return
	}
	entityID, err := normalizeIdentifier(p.EntityID, maxEntityID)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_entity", err.Error())
		return
	}
	entityType, err := normalizeIdentifier(p.Type, maxEntityID)
	if err != nil {
		s.sendError(c, frame.Channel, "invalid_entity", "invalid entity type")
		return
	}
	pos := state.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	if !validPosition(pos) {
		s.sendError(c, frame.Channel, "invalid_entity", "entity position is out of range")
		return
	}

	entity := s.state.UpsertEntity(entityID, entityType, pos, c.uuid, c.username, now)
	payload, err := marshalJSON(protocol.EntityStatePayload{
		EntityID:      entity.ID,
		Type:          entity.Type,
		X:             entity.Position.X,
		Y:             entity.Position.Y,
		Z:             entity.Position.Z,
		AuthorityID:   entity.AuthorityID,
		AuthorityName: entity.AuthorityName,
		ServerTime:    now.UnixMilli(),
	})
	if err != nil {
		return
	}
	s.broadcast(protocol.ChannelEntityUpdate, payload, c.uuid)
}

func (s *Server) handleAttack(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.AttackPayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed attack payload")
		return
	}
	targetID := strings.TrimSpace(p.TargetEntityID)
	if targetID != "" {
		normalized, err := normalizeIdentifier(targetID, maxEntityID)
		if err != nil {
			s.sendError(c, frame.Channel, "invalid_attack", "invalid target entity id")
			return
		}
		targetID = normalized
	}

	player, ok := s.state.GetPlayer(c.uuid)
	if !ok {
		return
	}
	if player.LastMovement.IsZero() {
		s.sendError(c, frame.Channel, "position_required", "send a position update before attacking")
		return
	}

	target := state.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	if targetID != "" {
		if entity, found := s.state.GetEntity(targetID); found {
			target = entity.Position
		}
	}
	if !validPosition(target) {
		s.sendError(c, frame.Channel, "invalid_attack", "target position is out of range")
		return
	}
	if !withinReach(player.Position, target, s.cfg.MaxReach) {
		s.sendError(c, frame.Channel, "reach", fmt.Sprintf("target is farther than %.1f blocks", s.cfg.MaxReach))
		return
	}
	if allowed, remaining := s.ac.CheckAttack(c.uuid, s.cfg.AttackCooldown, now); !allowed {
		s.sendError(c, frame.Channel, "attack_cooldown", fmt.Sprintf("wait %d ms", remaining.Milliseconds()))
		return
	}

	payload, err := marshalJSON(protocol.AttackEventPayload{
		ActorID:        player.ID,
		ActorName:      player.Username,
		TargetEntityID: targetID,
		X:              target.X,
		Y:              target.Y,
		Z:              target.Z,
		ServerTime:     now.UnixMilli(),
	})
	if err != nil {
		return
	}
	s.broadcast(protocol.ChannelAttack, payload, c.uuid)
}

func (s *Server) handleWorldState(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.WorldStatePayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil {
		s.sendError(c, frame.Channel, "invalid_payload", "malformed world_state payload")
		return
	}
	if len(p.Weather) > 64 || p.BorderDiameter <= 0 || !finite(p.SpawnX) || !finite(p.SpawnY) || !finite(p.SpawnZ) {
		s.sendError(c, frame.Channel, "invalid_world", "invalid world state")
		return
	}
	w := s.state.SetWorld(state.WorldState{
		TimeOfDay:      p.TimeOfDay,
		Weather:        p.Weather,
		SpawnX:         p.SpawnX,
		SpawnY:         p.SpawnY,
		SpawnZ:         p.SpawnZ,
		BorderDiameter: p.BorderDiameter,
	}, now)
	payload, err := marshalJSON(worldStatePayload(w, now))
	if err != nil {
		return
	}
	s.broadcast(protocol.ChannelWorldState, payload, c.uuid)
}

func (s *Server) handlePing(c *Client, frame protocol.Frame, now time.Time) {
	s.send(c, protocol.ChannelPong, frame.Payload)
}

func (s *Server) handlePong(c *Client, frame protocol.Frame, now time.Time) {
	var p protocol.PingPayload
	if err := unmarshalJSON(frame.Payload, &p); err != nil || p.Time <= 0 || p.Time > now.UnixMilli() {
		s.state.Touch(c.uuid, now)
		return
	}
	s.state.SetPing(c.uuid, now.Sub(time.UnixMilli(p.Time)), now)
}

func (s *Server) sendBlockLog(c *Client) {
	events := s.state.BlockEvents()
	out := make([]protocol.BlockChangeEvent, 0, len(events))
	for _, e := range events {
		out = append(out, blockEventPayload(e))
	}
	payload, err := marshalJSON(protocol.BlockLogPayload{Events: out})
	if err != nil {
		return
	}
	s.send(c, protocol.ChannelBlockLog, payload)
}

func (s *Server) sendWorldState(c *Client) {
	now := time.Now()
	payload, err := marshalJSON(worldStatePayload(s.state.World(), now))
	if err != nil {
		return
	}
	s.send(c, protocol.ChannelWorldState, payload)
}

func (s *Server) addPending(chunkID, requesterID string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requesters := s.pending[chunkID]
	if requesters == nil {
		requesters = make(map[string]time.Time)
		s.pending[chunkID] = requesters
	}
	requesters[requesterID] = now
}

func (s *Server) dropPending(chunkID, requesterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requesters := s.pending[chunkID]
	if requesters == nil {
		return
	}
	delete(requesters, requesterID)
	if len(requesters) == 0 {
		delete(s.pending, chunkID)
	}
}

func (s *Server) takePending(chunkID string) map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	requesters := s.pending[chunkID]
	delete(s.pending, chunkID)
	if len(requesters) == 0 {
		return nil
	}
	return requesters
}

func blockEventPayload(e state.BlockEvent) protocol.BlockChangeEvent {
	return protocol.BlockChangeEvent{
		Sequence:   e.Sequence,
		ActorID:    e.ActorID,
		ActorName:  e.ActorName,
		World:      e.World,
		X:          e.X,
		Y:          e.Y,
		Z:          e.Z,
		BlockID:    e.BlockID,
		ServerTime: e.ServerTime,
	}
}

func worldStatePayload(w state.WorldState, now time.Time) protocol.WorldStatePayload {
	return protocol.WorldStatePayload{
		TimeOfDay:      w.TimeOfDay,
		Weather:        w.Weather,
		SpawnX:         w.SpawnX,
		SpawnY:         w.SpawnY,
		SpawnZ:         w.SpawnZ,
		BorderDiameter: w.BorderDiameter,
		ServerTime:     now.UnixMilli(),
	}
}

func normalizeAndValidateConnect(meta *protocol.ConnectPayload) error {
	id, err := normalizeUUID(meta.UUID)
	if err != nil {
		return err
	}
	meta.UUID = id

	meta.Username = strings.TrimSpace(meta.Username)
	if meta.Username == "" || utf8.RuneCountInString(meta.Username) > 32 || !utf8.ValidString(meta.Username) {
		return fmt.Errorf("username must be 1-32 valid UTF-8 characters")
	}
	for _, r := range meta.Username {
		if unicode.IsControl(r) {
			return fmt.Errorf("username contains control characters")
		}
	}

	meta.GameMode = strings.ToLower(strings.TrimSpace(meta.GameMode))
	if meta.GameMode == "" {
		meta.GameMode = "survival"
	}
	switch meta.GameMode {
	case "survival", "creative", "adventure", "spectator":
	default:
		return fmt.Errorf("unsupported game mode %q", meta.GameMode)
	}

	meta.World = strings.TrimSpace(meta.World)
	if meta.World == "" {
		meta.World = "overworld"
	}
	world, err := normalizeIdentifier(meta.World, 64)
	if err != nil {
		return fmt.Errorf("invalid world: %w", err)
	}
	meta.World = world
	return nil
}

func normalizeUUID(value string) (string, error) {
	cleaned := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "")
	if len(cleaned) != 32 {
		return "", fmt.Errorf("uuid must be 32 hexadecimal digits")
	}
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("uuid must be 32 hexadecimal digits")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

func normalizeIdentifier(value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("identifier must not be empty")
	}
	if len(value) > max {
		return "", fmt.Errorf("identifier is longer than %d bytes", max)
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", fmt.Errorf("identifier contains whitespace or control characters")
		}
	}
	return value, nil
}

func validPosition(p state.Vec3) bool {
	return p.Valid() &&
		math.Abs(p.X) <= maxCoordinate &&
		math.Abs(p.Z) <= maxCoordinate &&
		math.Abs(p.Y) <= maxCoordinate
}

func withinReach(from, target state.Vec3, reach float64) bool {
	return from.Distance(target) <= reach
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func outOfRange(v int) bool {
	return v < -maxCoordinate || v > maxCoordinate
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func unmarshalJSON(data []byte, v any) error {
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	return json.Unmarshal(data, v)
}
