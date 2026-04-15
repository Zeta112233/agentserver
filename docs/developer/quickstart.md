# Quickstart: Build a Custom Agent in 5 Minutes

This guide gets a custom agent running with a "Hello World" Web UI served through the agentserver proxy.

## Prerequisites

- Go 1.22 or later
- Access to an agentserver instance (you need a workspace invite or developer access)
- The server URL (e.g. `https://agent.example.com`)

## Step 1 — Create a Go module

```bash
mkdir my-agent && cd my-agent
go mod init my-agent
go get github.com/agentserver/agentserver/pkg/agentsdk@latest
```

## Step 2 — Write the agent

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"

    "github.com/agentserver/agentserver/pkg/agentsdk"
)

func main() {
    serverURL := os.Getenv("AGENTSERVER_URL")
    if serverURL == "" {
        log.Fatal("Set AGENTSERVER_URL to your agentserver address")
    }

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    // Authenticate via browser.
    deviceResp, err := agentsdk.RequestDeviceCode(ctx, serverURL)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("\nOpen this URL to authenticate:\n\n    %s\n\n", deviceResp.VerificationURIComplete)

    tokenResp, err := agentsdk.PollForToken(ctx, serverURL, deviceResp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Authenticated!")

    // Register the agent.
    client := agentsdk.NewClient(agentsdk.Config{
        ServerURL: serverURL,
        Name:      "Hello World Agent",
    })
    reg, err := client.Register(ctx, tokenResp.AccessToken)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Registered! short_id=%s\n", reg.ShortID)

    // Connect and serve HTTP.
    err = client.Connect(ctx, agentsdk.Handlers{
        HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "text/html")
            fmt.Fprintf(w, "<h1>Hello from my agent!</h1><p>Path: %s</p>", r.URL.Path)
        }),
        OnConnect:    func() { log.Println("Tunnel connected") },
        OnDisconnect: func(err error) { log.Printf("Tunnel disconnected: %v", err) },
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

## Step 3 — Run it

```bash
AGENTSERVER_URL=https://agent.example.com go run .
```

The program prints a URL. Open it in your browser, log in, and approve access. Then:

```
Authenticated!
Registered! short_id=xk9mTqLpRwZvNcYu
Tunnel connected
```

## Step 4 — Visit your Web UI

Open `https://code-{shortID}.{baseDomain}` in a browser. You should see the "Hello from my agent!" page. The `shortID` is printed in the terminal after registration.

If your agentserver is at `https://agent.example.com`, the Web UI subdomain is under the same base domain: `https://code-xk9mTqLpRwZvNcYu.example.com`.

## What happens under the hood

1. `RequestDeviceCode` + `PollForToken` run the OAuth 2.0 Device Authorization Flow. You authenticate in a browser; the token comes back to your process.
2. `client.Register` calls `POST /api/agent/register` and returns a `sandbox_id`, `tunnel_token`, `proxy_token`, and `short_id`.
3. `client.Connect` establishes a WebSocket to `wss://{server}/api/tunnel/{sandbox_id}?token={tunnel_token}` and creates a yamux multiplexing session over it.
4. When a user visits `code-{shortID}.{baseDomain}`, the sandboxproxy opens a yamux stream, serializes the HTTP request into the stream, and your `HTTP` handler receives a normal `*http.Request`. The response is written back through the stream to the user.
5. A heartbeat goroutine sends a control stream every 20 seconds to keep the connection alive and update your agent's online status.
6. On disconnection, `Connect` automatically reconnects with exponential backoff (1s → 60s, reset after 30s uptime).

## Next steps

- **Serve a real UI**: Replace the inline handler with `http.FileServer` or your own router.
- **Handle tasks**: Add a `Task` handler to `agentsdk.Handlers` to receive work from other agents. See [protocol.md — Task Execution](protocol.md#7-task-execution).
- **Declare capabilities**: Call `POST /api/agent/discovery/cards` with your `proxy_token` so the Web UI can list your agent's skills. See [protocol.md — Agent Discovery](protocol.md#8-agent-discovery).
- **Persist credentials**: Save `reg.SandboxID`, `reg.TunnelToken`, and `reg.ProxyToken` to disk and use `client.SetRegistration` on subsequent runs to keep the same `short_id`.
- **Non-Go agents**: See [protocol.md](protocol.md) for the full binary protocol. Any language with WebSocket and yamux library support can implement a custom agent.
