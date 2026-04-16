package ccbroker

import "sync"

type TurnLock struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

func NewTurnLock() *TurnLock {
	return &TurnLock{locks: make(map[string]chan struct{})}
}

func (t *TurnLock) Acquire(sessionID string) {
	t.mu.Lock()
	ch, exists := t.locks[sessionID]
	if !exists {
		ch = make(chan struct{}, 1)
		t.locks[sessionID] = ch
	}
	t.mu.Unlock()
	ch <- struct{}{}
}

func (t *TurnLock) Release(sessionID string) {
	t.mu.Lock()
	ch, exists := t.locks[sessionID]
	t.mu.Unlock()
	if exists {
		<-ch
	}
}
