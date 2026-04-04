package server

import (
	"context"
	"log"
	"time"

	"github.com/agentserver/agentserver/internal/db"
)

// AgentHealthMonitor periodically marks stale agents as offline.
type AgentHealthMonitor struct {
	db       *db.DB
	interval time.Duration // sweep interval (30s)
	offline  time.Duration // heartbeat threshold (60s)
}

func NewAgentHealthMonitor(database *db.DB) *AgentHealthMonitor {
	return &AgentHealthMonitor{
		db:       database,
		interval: 30 * time.Second,
		offline:  60 * time.Second,
	}
}

// Run blocks until ctx is cancelled.
func (m *AgentHealthMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := m.db.MarkStaleAgentCardsOffline(m.offline)
			if err != nil {
				log.Printf("agent-health: mark offline error: %v", err)
			} else if n > 0 {
				log.Printf("agent-health: marked %d agents offline", n)
			}
		}
	}
}
