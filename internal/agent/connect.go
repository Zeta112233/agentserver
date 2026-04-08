package agent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ConnectOptions holds all flags for the connect command.
type ConnectOptions struct {
	Server          string
	HydraURL        string
	Name            string
	WorkspaceID     string // optional: disambiguate when dir has multiple workspaces
	SkipOpenBrowser bool
	OpencodeURL     string
	OpencodeURLSet  bool // true if --opencode-url was explicitly provided
	OpencodeToken   string
	AutoStart       bool
	OpencodeBin     string
	OpencodePort    int  // 0 = auto-assign from registry
	OpencodePortSet bool // true if --opencode-port was explicitly provided
}

// RunConnect executes the agent connect workflow.
func RunConnect(opts ConnectOptions) {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	registryPath := DefaultRegistryPath()

	// Lock registry for the read-modify-write cycle.
	locked, err := LockRegistry(registryPath)
	if err != nil {
		log.Fatalf("Failed to load registry: %v", err)
	}
	defer locked.Close()

	reg := locked.Reg

	// Check if we need to register (no saved credentials for this directory).
	entries := reg.FindByDir(cwd)
	if len(entries) == 0 {
		// --- New registration via OAuth Device Flow ---
		if opts.Server == "" {
			log.Fatal("--server is required for registration")
		}
		if opts.HydraURL == "" {
			log.Fatal("--hydra-url is required for registration")
		}
		locked.Close() // Release lock during interactive login.

		if err := RunLogin(LoginOptions{
			ServerURL:       opts.Server,
			HydraPublicURL:  opts.HydraURL,
			Name:            opts.Name,
			Type:            "opencode",
			SkipOpenBrowser: opts.SkipOpenBrowser,
		}); err != nil {
			log.Fatalf("Login failed: %v", err)
		}

		// Re-lock and reload registry.
		locked, err = LockRegistry(registryPath)
		if err != nil {
			log.Fatalf("Failed to reload registry: %v", err)
		}
		defer locked.Close()
		reg = locked.Reg
		entries = reg.FindByDir(cwd)
		if len(entries) == 0 {
			log.Fatal("Registration succeeded but no entry found in registry")
		}
	}

	// Select entry.
	var entry *RegistryEntry
	switch len(entries) {
	case 1:
		entry = entries[0]
	default:
		if opts.WorkspaceID == "" {
			log.Printf("Multiple workspaces registered for this directory:")
			for _, e := range entries {
				log.Printf("  workspace=%s  name=%s  sandbox=%s", e.WorkspaceID, e.Name, e.SandboxID)
			}
			log.Fatal("Use --workspace to specify which one to connect.")
		}
		entry = reg.Find(cwd, opts.WorkspaceID)
		if entry == nil {
			log.Fatalf("No entry found for workspace %q in this directory", opts.WorkspaceID)
		}
	}
	log.Printf("Using credentials (sandbox: %s)", entry.SandboxID)
	if opts.Server != "" {
		entry.Server = opts.Server
	}

	// Assign opencode port if not yet set (new registration via RunLogin doesn't set it).
	if entry.OpencodePort == 0 && !opts.OpencodePortSet {
		entry.OpencodePort = reg.NextPort()
		reg.Put(entry)
		if err := locked.Save(); err != nil {
			log.Printf("Warning: failed to save port assignment: %v", err)
		}
	}

	// Determine opencode port: command-line override or entry value.
	opencodePort := entry.OpencodePort
	if opts.OpencodePortSet {
		opencodePort = opts.OpencodePort
		// Persist the override so subsequent reconnects use the same port.
		if entry.OpencodePort != opencodePort {
			entry.OpencodePort = opencodePort
			reg.Put(entry)
			if err := locked.Save(); err != nil {
				log.Printf("Warning: failed to save port override: %v", err)
			}
		}
	}

	// Auto-start opencode if requested.
	var opencodeProc *OpencodeProcess
	if opts.AutoStart {
		opencodeURL := fmt.Sprintf("http://localhost:%d", opencodePort)

		// Check if opencode is already listening.
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(opencodeURL + "/")
		if err == nil {
			resp.Body.Close()
			log.Printf("opencode already running on port %d, skipping auto-start", opencodePort)
		} else {
			log.Printf("Starting opencode on port %d...", opencodePort)
			opencodeProc, err = StartOpencode(opts.OpencodeBin, opencodePort, opts.OpencodeToken)
			if err != nil {
				log.Fatalf("Failed to start opencode: %v", err)
			}

			readyCtx, readyCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := opencodeProc.WaitReady(readyCtx, 30*time.Second); err != nil {
				readyCancel()
				opencodeProc.Stop()
				log.Fatalf("opencode failed to become ready: %v", err)
			}
			readyCancel()
		}

		// Use the auto-started URL unless --opencode-url was explicitly set.
		if !opts.OpencodeURLSet {
			opts.OpencodeURL = opencodeURL
		}
	}

	if opts.OpencodeURL == "" {
		opts.OpencodeURL = fmt.Sprintf("http://localhost:%d", opencodePort)
	}

	tunnelClient := NewClient(entry.Server, entry.SandboxID, entry.TunnelToken, opts.OpencodeURL, opts.OpencodeToken, cwd)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh
		log.Printf("Received %v, disconnecting...", sig)
		cancel()

		// Stop opencode subprocess if we started it.
		if opencodeProc != nil {
			opencodeProc.Stop()
		}
	}()

	log.Printf("Connecting to %s (forwarding to %s)...", entry.Server, opts.OpencodeURL)
	if err := tunnelClient.Run(ctx); err != nil && ctx.Err() == nil {
		if opencodeProc != nil {
			opencodeProc.Stop()
		}
		log.Fatalf("Agent error: %v", err)
	}

	// Clean up opencode on normal exit too.
	if opencodeProc != nil {
		opencodeProc.Stop()
	}
	log.Println("Agent disconnected.")
}
