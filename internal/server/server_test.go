package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"smsinecraft/internal/config"
	"smsinecraft/internal/protocol"
	"smsinecraft/internal/server"
)

const (
	uuidA = "11111111-1111-4111-8111-111111111111"
	uuidB = "22222222-2222-4222-8222-222222222222"
	uuidC = "33333333-3333-4333-8333-333333333333"
)

func startServer(t *testing.T) string {
	t.Helper()
	cfg := config.Default()
	cfg.Addr = "127.0.0.1:0"
	cfg.RateLimitPerSecond = 1000
	cfg.RateLimitBurst = 1000
	cfg.PingInterval = 5 * time.Second
	cfg.IdleTimeout = 30 * time.Second
	cfg.AttackCooldown = 10 * time.Millisecond
	cfg.Movement.SurvivalMaxSpeed = 1000
	cfg.Movement.CreativeMaxSpeed = 1000
	cfg.Movement.SpectatorMaxSpeed = 1000
	cfg.Movement.MaxVerticalSpeed = 1000

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server returned error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ln.Addr().String()
}

func connect(t *testing.T, addr, id, username string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	sendJSON(t, conn, protocol.ChannelConnect, protocol.ConnectPayload{
		UUID:     id,
		Username: username,
		GameMode: "survival",
		World:    "overworld",
	})
	frame := waitChannel(t, conn, protocol.ChannelConnectAck)
	var ack protocol.ConnectAckPayload
	decode(t, frame, &ack)
	if ack.PlayerID != id {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	return conn
}

func TestRelayFlow(t *testing.T) {
	addr := startServer(t)
	a := connect(t, addr, uuidA, "Alice")
	b := connect(t, addr, uuidB, "Bob")

	waitPlayerCount(t, a, 2)
	waitPlayerCount(t, b, 2)

	sendJSON(t, a, protocol.ChannelPosition, protocol.PositionUpdatePayload{X: 0, Y: 64, Z: 0, OnGround: true})
	pos := waitChannel(t, b, protocol.ChannelPosition)
	var posState protocol.PositionStatePayload
	decode(t, pos, &posState)
	if posState.PlayerID != uuidA {
		t.Fatalf("unexpected position broadcast: %+v", posState)
	}

	sendJSON(t, b, protocol.ChannelPosition, protocol.PositionUpdatePayload{X: 0.5, Y: 64, Z: 0.5, OnGround: true})
	sendJSON(t, b, protocol.ChannelBlockChange, protocol.BlockChangePayload{
		X: 1, Y: 64, Z: 0, BlockID: "minecraft:stone",
	})
	block := waitChannel(t, a, protocol.ChannelBlockChange)
	var blockEvent protocol.BlockChangeEvent
	decode(t, block, &blockEvent)
	if blockEvent.ActorID != uuidB || blockEvent.BlockID != "minecraft:stone" {
		t.Fatalf("unexpected block event: %+v", blockEvent)
	}

	sendJSON(t, b, protocol.ChannelBlockChange, protocol.BlockChangePayload{
		X: 100, Y: 64, Z: 0, BlockID: "minecraft:stone",
	})
	errFrame := waitChannel(t, b, protocol.ChannelError)
	var serverErr protocol.ErrorPayload
	decode(t, errFrame, &serverErr)
	if serverErr.Code != "reach" {
		t.Fatalf("expected reach error, got %+v", serverErr)
	}

	sendJSON(t, a, protocol.ChannelChunkProduced, protocol.ChunkProducedPayload{
		ChunkID: "overworld:0,0", Summary: []byte("summary-a"),
	})
	authority := waitChannel(t, a, protocol.ChannelChunkAuthority)
	var auth protocol.ChunkAuthorityPayload
	decode(t, authority, &auth)
	if auth.Status != protocol.ChunkStatusAccepted || auth.OwnerID != uuidA {
		t.Fatalf("unexpected authority: %+v", auth)
	}

	sendJSON(t, b, protocol.ChannelChunkProduced, protocol.ChunkProducedPayload{
		ChunkID: "overworld:0,0", Summary: []byte("summary-b"),
	})
	conflict := waitChannel(t, b, protocol.ChannelChunkAuthority)
	decode(t, conflict, &auth)
	if auth.Status != protocol.ChunkStatusConflict || auth.OwnerID != uuidA {
		t.Fatalf("expected chunk conflict: %+v", auth)
	}

	sendJSON(t, b, protocol.ChannelChunkRequest, protocol.ChunkRequestPayload{ChunkID: "overworld:0,0"})
	request := waitChannel(t, a, protocol.ChannelChunkRequest)
	var requestRelay protocol.ChunkRequestRelayPayload
	decode(t, request, &requestRelay)
	if requestRelay.RequesterID != uuidB {
		t.Fatalf("unexpected chunk request: %+v", requestRelay)
	}
	sendJSON(t, a, protocol.ChannelChunkResponse, protocol.ChunkResponsePayload{
		ChunkID: "overworld:0,0", Data: []byte("opaque-chunk-bytes"),
	})
	response := waitChannel(t, b, protocol.ChannelChunkResponse)
	var responseRelay protocol.ChunkResponseRelayPayload
	decode(t, response, &responseRelay)
	if responseRelay.FromID != uuidA || !bytes.Equal(responseRelay.Data, []byte("opaque-chunk-bytes")) {
		t.Fatalf("unexpected chunk response: %+v", responseRelay)
	}

	c := connect(t, addr, uuidC, "Carol")
	logFrame := waitChannel(t, c, protocol.ChannelBlockLog)
	var logPayload protocol.BlockLogPayload
	decode(t, logFrame, &logPayload)
	if len(logPayload.Events) != 1 || logPayload.Events[0].ActorID != uuidB {
		t.Fatalf("late joiner did not receive block log: %+v", logPayload)
	}
	waitPlayerCount(t, c, 3)
}

func TestConnectRejectsInvalidFirstPacket(t *testing.T) {
	addr := startServer(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	sendJSON(t, conn, protocol.ChannelPosition, protocol.PositionUpdatePayload{})
	frame := waitChannel(t, conn, protocol.ChannelError)
	var p protocol.ErrorPayload
	decode(t, frame, &p)
	if p.Code != "connect_required" {
		t.Fatalf("unexpected error: %+v", p)
	}
}

func sendJSON(t *testing.T, conn net.Conn, channel string, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := protocol.WriteFrame(conn, protocol.Frame{Channel: channel, Payload: data}); err != nil {
		t.Fatal(err)
	}
}

func readFrame(t *testing.T, conn net.Conn) protocol.Frame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := protocol.ReadFrame(conn, 2<<20)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

func waitChannel(t *testing.T, conn net.Conn, channel string) protocol.Frame {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		frame := readFrame(t, conn)
		if frame.Channel == channel {
			return frame
		}
	}
	t.Fatalf("did not receive channel %s", channel)
	return protocol.Frame{}
}

func waitPlayerCount(t *testing.T, conn net.Conn, count int) protocol.PlayerListPayload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		frame := readFrame(t, conn)
		if frame.Channel != protocol.ChannelPlayerList {
			continue
		}
		var p protocol.PlayerListPayload
		decode(t, frame, &p)
		if len(p.Players) == count {
			return p
		}
	}
	t.Fatalf("did not receive player list with %d players", count)
	return protocol.PlayerListPayload{}
}

func decode(t *testing.T, frame protocol.Frame, v any) {
	t.Helper()
	if err := json.Unmarshal(frame.Payload, v); err != nil {
		t.Fatalf("decode %s: %v", frame.Channel, err)
	}
}
