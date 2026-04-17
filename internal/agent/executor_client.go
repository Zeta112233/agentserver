package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"nhooyr.io/websocket"

	"github.com/agentserver/agentserver/internal/agent/executortools"
	"github.com/agentserver/agentserver/internal/tunnel"
)

// ExecutorClient runs a tunnel to executor-registry and serves tool
// execution requests from cc-broker workers.
type ExecutorClient struct {
	session  *ExecutorSession
	workDir  string
	executor *executortools.ToolExecutor
}

// NewExecutorClient constructs a new executor client bound to the given
// registry session and working directory.
func NewExecutorClient(sess *ExecutorSession, workDir string) *ExecutorClient {
	return &ExecutorClient{
		session:  sess,
		workDir:  workDir,
		executor: executortools.New(workDir),
	}
}

// Run maintains a persistent tunnel to executor-registry and reconnects
// with exponential backoff on disconnection.
func (c *ExecutorClient) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		connectedAt := time.Now()
		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Printf("tunnel disconnected: %v", err)
		}

		if time.Since(connectedAt) > 30*time.Second {
			backoff = time.Second
		}

		log.Printf("reconnecting in %s...", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *ExecutorClient) connectAndServe(ctx context.Context) error {
	wsURL := httpToWS(c.session.ServerURL) + "/api/tunnel/" + c.session.ExecutorID + "?token=" + c.session.TunnelToken

	log.Printf("connecting to %s", wsURL)

	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	log.Printf("tunnel connected (executor: %s)", c.session.ExecutorID)

	conn := tunnel.NewWSConn(ctx, ws)
	session, err := tunnel.ClientMux(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("yamux session: %w", err)
	}
	defer session.Close()

	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go c.heartbeatLoop(hbCtx)

	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accept stream: %w", err)
		}
		go c.handleStream(ctx, stream)
	}
}

// handleStream reads one HTTP request from the stream and responds.
// Task 3 wires the real tool dispatcher; for now every request returns 501.
func (c *ExecutorClient) handleStream(ctx context.Context, stream net.Conn) {
	defer stream.Close()

	req, err := http.ReadRequest(bufio.NewReader(stream))
	if err != nil {
		if err != io.EOF {
			log.Printf("read request: %v", err)
		}
		return
	}
	defer req.Body.Close()

	if req.Method != http.MethodPost || req.URL.Path != "/tool/execute" {
		writeHTTPResponse(stream, http.StatusNotFound, []byte(`{"error":"not found"}`))
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeHTTPResponse(stream, http.StatusBadRequest, []byte(`{"error":"read body"}`))
		return
	}

	var execReq executortools.ExecuteRequest
	if err := json.Unmarshal(body, &execReq); err != nil {
		writeHTTPResponse(stream, http.StatusBadRequest, []byte(`{"error":"invalid body"}`))
		return
	}

	resp := c.executor.Execute(ctx, execReq)
	respBody, _ := json.Marshal(resp)
	writeHTTPResponse(stream, http.StatusOK, respBody)
}

// heartbeatLoop sends a heartbeat every 20s (plus one immediately).
func (c *ExecutorClient) heartbeatLoop(ctx context.Context) {
	c.sendHeartbeat(ctx)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendHeartbeat(ctx)
		}
	}
}

func (c *ExecutorClient) sendHeartbeat(ctx context.Context) {
	info := collectAgentInfo("", c.workDir)
	infoJSON, _ := json.Marshal(info)

	body, _ := json.Marshal(map[string]interface{}{
		"status":      "online",
		"system_info": json.RawMessage(infoJSON),
		"capabilities": map[string]interface{}{
			"tools":       []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "LS"},
			"working_dir": c.workDir,
			"description": "Local machine executor",
		},
	})

	url := c.session.ServerURL + "/api/executors/" + c.session.ExecutorID + "/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("heartbeat build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.session.RegistryToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("heartbeat failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("heartbeat returned %d: %s", resp.StatusCode, respBody)
	}
}

// writeHTTPResponse writes a minimal HTTP/1.1 response over the stream.
func writeHTTPResponse(w io.Writer, status int, body []byte) {
	resp := &http.Response{
		StatusCode:    status,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	_ = resp.Write(w)
}

// httpToWS converts http(s):// → ws(s):// preserving the remainder.
func httpToWS(u string) string {
	if strings.HasPrefix(u, "https://") {
		return "wss://" + strings.TrimPrefix(u, "https://")
	}
	if strings.HasPrefix(u, "http://") {
		return "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u
}
