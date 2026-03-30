package tunnel

import (
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

// MuxConfig returns the yamux configuration for the tunnel.
func MuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	// Disable yamux's built-in keepalive — we do our own heartbeat via
	// periodic agent-info control streams, which serve double duty as
	// keepalive traffic and metadata refresh.
	cfg.EnableKeepAlive = false
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.AcceptBacklog = 256
	cfg.LogOutput = io.Discard
	return cfg
}

// ServerMux creates a yamux server session over conn.
// The agentserver side acts as the yamux server: it accepts streams
// opened by the agent (control messages) and opens streams towards
// the agent (HTTP proxy, terminal).
func ServerMux(conn net.Conn) (*yamux.Session, error) {
	return yamux.Server(conn, MuxConfig())
}

// ClientMux creates a yamux client session over conn.
// The local agent acts as the yamux client: it opens streams towards
// the server (control messages) and accepts streams from the server
// (HTTP proxy, terminal).
func ClientMux(conn net.Conn) (*yamux.Session, error) {
	return yamux.Client(conn, MuxConfig())
}
