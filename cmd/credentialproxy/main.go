package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentserver/agentserver/internal/credentialproxy"
	"github.com/agentserver/agentserver/internal/credentialproxy/k8s"
)

func main() {
	cfg, err := credentialproxy.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := credentialproxy.NewLogger(cfg.LogLevel)

	// Configure K8s provider settings.
	k8s.AllowPrivateUpstreams = cfg.AllowPrivateUpstreams
	if cfg.AllowPrivateUpstreams {
		logger.Warn("SSRF dial guard disabled: CREDPROXY_ALLOW_PRIVATE_UPSTREAMS=true")
	}

	store, err := credentialproxy.NewStore(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer store.Close()
	logger.Info("connected to database")

	srv := credentialproxy.NewServer(cfg, store, logger)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv.Routes(),
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
	}()

	logger.Info("starting credentialproxy", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
