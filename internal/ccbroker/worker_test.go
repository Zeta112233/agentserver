package ccbroker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTurnLock(t *testing.T) {
	tl := NewTurnLock()

	// Acquire for session A
	tl.Acquire("session-a")

	// Try to acquire in goroutine — should block
	acquired := make(chan struct{})
	go func() {
		tl.Acquire("session-a")
		close(acquired)
		tl.Release("session-a")
	}()

	// Should not acquire within 100ms
	select {
	case <-acquired:
		t.Fatal("second acquire should block")
	case <-time.After(100 * time.Millisecond):
		// expected
	}

	// Release — goroutine should unblock
	tl.Release("session-a")

	select {
	case <-acquired:
		// expected
	case <-time.After(time.Second):
		t.Fatal("second acquire should have unblocked after release")
	}
}

func TestTurnLock_DifferentSessions(t *testing.T) {
	tl := NewTurnLock()

	tl.Acquire("session-a")

	// Acquiring for a different session should NOT block
	acquired := make(chan struct{})
	go func() {
		tl.Acquire("session-b")
		close(acquired)
		tl.Release("session-b")
	}()

	select {
	case <-acquired:
		// expected — different sessions don't block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("different session should not block")
	}

	tl.Release("session-a")
}

func TestFileSnapshot(t *testing.T) {
	dir := t.TempDir()

	// Create initial files
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "file2.txt"), []byte("world"), 0644)

	// Take snapshot
	snap := takeFileSnapshot(dir)
	if len(snap) != 2 {
		t.Fatalf("expected 2 files in snapshot, got %d", len(snap))
	}

	// Wait a moment (modtime resolution)
	time.Sleep(10 * time.Millisecond)

	// Modify file1, add file3
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello modified"), 0644)
	os.WriteFile(filepath.Join(dir, "file3.txt"), []byte("new file"), 0644)

	// Diff
	changes := diffSnapshot(dir, snap)

	// Should have 2 changes: file1.txt modified, file3.txt new
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}

	changeMap := make(map[string]fileChange)
	for _, c := range changes {
		changeMap[c.RelPath] = c
	}

	if _, ok := changeMap["file1.txt"]; !ok {
		t.Error("missing change for file1.txt")
	}
	if c, ok := changeMap["file3.txt"]; !ok || !c.IsNew {
		t.Error("file3.txt should be new")
	}
}
