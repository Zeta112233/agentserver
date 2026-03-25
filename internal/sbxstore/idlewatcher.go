package sbxstore

import (
	"log"
	"time"

	"github.com/agentserver/agentserver/internal/db"
	"github.com/agentserver/agentserver/internal/process"
)

// IdleWatcher monitors sandboxes and auto-pauses idle ones.
type IdleWatcher struct {
	db         *db.DB
	procMgr    process.Manager
	store      *Store
	getTimeout func() time.Duration
	onPrePause func(sandboxID string) // called before pausing a sandbox (e.g. to stop bridge pollers)
	stop       chan struct{}
}

// NewIdleWatcher creates a new idle sandbox watcher.
// The getTimeout function is called each check cycle to resolve the current idle timeout.
// If it returns 0, idle checking is skipped (disabled).
func NewIdleWatcher(database *db.DB, procMgr process.Manager, store *Store, getTimeout func() time.Duration) *IdleWatcher {
	return &IdleWatcher{
		db:         database,
		procMgr:    procMgr,
		store:      store,
		getTimeout: getTimeout,
		stop:       make(chan struct{}),
	}
}

// SetOnPrePause sets a callback that is invoked before a sandbox is paused.
// Used to stop WeChat bridge pollers before the Pod goes away.
func (w *IdleWatcher) SetOnPrePause(fn func(sandboxID string)) {
	w.onPrePause = fn
}

// Start begins the idle check loop. Call Stop() to terminate.
func (w *IdleWatcher) Start() {
	go w.loop()
}

// Stop terminates the idle watcher.
func (w *IdleWatcher) Stop() {
	close(w.stop)
}

func (w *IdleWatcher) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *IdleWatcher) check() {
	timeout := w.getTimeout()
	if timeout <= 0 {
		return // idle checking disabled
	}

	sandboxes, err := w.db.ListIdleSandboxes(int(timeout.Seconds()))
	if err != nil {
		log.Printf("idle watcher: failed to list idle sandboxes: %v", err)
		return
	}

	for _, sbx := range sandboxes {
		log.Printf("idle watcher: pausing idle sandbox %s (last activity: %v)", sbx.ID, sbx.LastActivityAt)

		// Pre-pause hook (stop bridge pollers, etc.)
		if w.onPrePause != nil {
			w.onPrePause(sbx.ID)
		}

		// Transition to pausing.
		if err := w.store.UpdateStatus(sbx.ID, StatusPausing); err != nil {
			log.Printf("idle watcher: failed to set pausing status for %s: %v", sbx.ID, err)
			continue
		}

		// Pause the process.
		if err := w.procMgr.Pause(sbx.ID); err != nil {
			log.Printf("idle watcher: failed to pause process for %s: %v", sbx.ID, err)
			// Revert status to running.
			w.store.UpdateStatus(sbx.ID, StatusRunning)
			continue
		}

		// Clear pod IP so the proxy won't connect to a stale address.
		if err := w.db.UpdateSandboxPodIP(sbx.ID, ""); err != nil {
			log.Printf("idle watcher: failed to clear pod IP for %s: %v", sbx.ID, err)
		}

		// Transition to paused.
		if err := w.store.UpdateStatus(sbx.ID, StatusPaused); err != nil {
			log.Printf("idle watcher: failed to set paused status for %s: %v", sbx.ID, err)
		}
	}
}
