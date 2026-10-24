package server

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"smsinecraft/internal/anticheat"
	"smsinecraft/internal/protocol"
)

type Client struct {
	server *Server
	conn   net.Conn

	send chan protocol.Frame
	done chan struct{}

	closeOnce  sync.Once
	writerOnce sync.Once
	closed     atomic.Bool
	queued     atomic.Int64

	uuid     string
	username string
	rate     *anticheat.RateLimiter

	lastRateWarn time.Time
}

func newClient(s *Server, conn net.Conn) *Client {
	return &Client{
		server: s,
		conn:   conn,
		send:   make(chan protocol.Frame, s.cfg.OutboundQueueFrames),
		done:   make(chan struct{}),
	}
}

func (c *Client) startWriter() {
	c.writerOnce.Do(func() {
		go c.writeLoop()
	})
}

func (c *Client) writeLoop() {
	defer c.close()
	for {
		select {
		case frame := <-c.send:
			size := frameSize(frame)
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.server.cfg.WriteTimeout))
			err := protocol.WriteFrame(c.conn, frame)
			c.queued.Add(-int64(size))
			if err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) enqueue(frame protocol.Frame) bool {
	if c.closed.Load() {
		return false
	}
	size := int64(frameSize(frame))
	if size > c.server.cfg.OutboundQueueBytes {
		c.close()
		return false
	}
	if c.queued.Add(size) > c.server.cfg.OutboundQueueBytes {
		c.queued.Add(-size)
		c.close()
		return false
	}

	select {
	case c.send <- frame:
		return true
	case <-c.done:
		c.queued.Add(-size)
		return false
	default:
		c.queued.Add(-size)
		c.close()
		return false
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(c.server.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			payload, err := marshalJSON(protocol.PingPayload{Time: time.Now().UnixMilli()})
			if err != nil {
				return
			}
			if !c.enqueue(c.server.newFrame(protocol.ChannelPing, payload)) {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) warnRateLimited(now time.Time, s *Server) {
	if now.Sub(c.lastRateWarn) < time.Second {
		return
	}
	c.lastRateWarn = now
	s.sendError(c, "", "rate_limited", "incoming packet rate exceeded")
}

func frameSize(f protocol.Frame) int {
	return 26 + len(f.Channel) + len(f.Payload)
}
