package agent

import (
	"path/filepath"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")

	// New registry, put an entry, save, reload, verify.
	reg := &Registry{}
	entry := &RegistryEntry{
		Dir:         "/home/user/project",
		Server:      "https://example.com",
		SandboxID:   "sbx-123",
		TunnelToken: "tok-abc",
		WorkspaceID: "ws-1",
		Name:        "my-agent",
	}
	reg.Put(entry)

	if err := SaveRegistry(regPath, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	loaded, err := LoadRegistry(regPath)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}

	got := loaded.Entries[0]
	if got.Dir != entry.Dir {
		t.Errorf("Dir = %q, want %q", got.Dir, entry.Dir)
	}
	if got.Server != entry.Server {
		t.Errorf("Server = %q, want %q", got.Server, entry.Server)
	}
	if got.SandboxID != entry.SandboxID {
		t.Errorf("SandboxID = %q, want %q", got.SandboxID, entry.SandboxID)
	}
	if got.TunnelToken != entry.TunnelToken {
		t.Errorf("TunnelToken = %q, want %q", got.TunnelToken, entry.TunnelToken)
	}
	if got.WorkspaceID != entry.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, entry.WorkspaceID)
	}
	if got.Name != entry.Name {
		t.Errorf("Name = %q, want %q", got.Name, entry.Name)
	}
}

func TestRegistryLookup(t *testing.T) {
	reg := &Registry{}
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/projectA",
		WorkspaceID: "ws-1",
		SandboxID:   "sbx-a1",
	})
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/projectA",
		WorkspaceID: "ws-2",
		SandboxID:   "sbx-a2",
	})
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/projectB",
		WorkspaceID: "ws-1",
		SandboxID:   "sbx-b1",
	})

	// Find by exact (dir, workspace).
	e := reg.Find("/home/user/projectA", "ws-1")
	if e == nil {
		t.Fatal("Find returned nil for existing entry")
	}
	if e.SandboxID != "sbx-a1" {
		t.Errorf("Find: SandboxID = %q, want %q", e.SandboxID, "sbx-a1")
	}

	// Find different workspace in same dir.
	e = reg.Find("/home/user/projectA", "ws-2")
	if e == nil {
		t.Fatal("Find returned nil for ws-2 entry")
	}
	if e.SandboxID != "sbx-a2" {
		t.Errorf("Find: SandboxID = %q, want %q", e.SandboxID, "sbx-a2")
	}

	// FindByDir returns all entries for a directory.
	entries := reg.FindByDir("/home/user/projectA")
	if len(entries) != 2 {
		t.Fatalf("FindByDir: got %d entries, want 2", len(entries))
	}

	entries = reg.FindByDir("/home/user/projectB")
	if len(entries) != 1 {
		t.Fatalf("FindByDir: got %d entries, want 1", len(entries))
	}

	// Miss cases.
	if e := reg.Find("/nonexistent", "ws-1"); e != nil {
		t.Error("Find should return nil for nonexistent dir")
	}
	if e := reg.Find("/home/user/projectA", "ws-999"); e != nil {
		t.Error("Find should return nil for nonexistent workspace")
	}
	if entries := reg.FindByDir("/nonexistent"); len(entries) != 0 {
		t.Error("FindByDir should return empty slice for nonexistent dir")
	}
}

func TestRegistryPutOverwrite(t *testing.T) {
	reg := &Registry{}
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/project",
		WorkspaceID: "ws-1",
		SandboxID:   "sbx-old",
		Name:        "old-name",
	})

	// Overwrite with same (dir, workspace).
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/project",
		WorkspaceID: "ws-1",
		SandboxID:   "sbx-new",
		Name:        "new-name",
	})

	if len(reg.Entries) != 1 {
		t.Fatalf("expected 1 entry after overwrite, got %d", len(reg.Entries))
	}

	e := reg.Find("/home/user/project", "ws-1")
	if e == nil {
		t.Fatal("Find returned nil after overwrite")
	}
	if e.SandboxID != "sbx-new" {
		t.Errorf("SandboxID = %q, want %q", e.SandboxID, "sbx-new")
	}
	if e.Name != "new-name" {
		t.Errorf("Name = %q, want %q", e.Name, "new-name")
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := &Registry{}
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/project",
		WorkspaceID: "ws-1",
		SandboxID:   "sbx-1",
	})
	reg.Put(&RegistryEntry{
		Dir:         "/home/user/project",
		WorkspaceID: "ws-2",
		SandboxID:   "sbx-2",
	})

	// Remove existing entry.
	ok := reg.Remove("/home/user/project", "ws-1")
	if !ok {
		t.Error("Remove returned false for existing entry")
	}
	if len(reg.Entries) != 1 {
		t.Fatalf("expected 1 entry after remove, got %d", len(reg.Entries))
	}
	if e := reg.Find("/home/user/project", "ws-1"); e != nil {
		t.Error("removed entry still found")
	}

	// Remaining entry is still there.
	if e := reg.Find("/home/user/project", "ws-2"); e == nil {
		t.Error("other entry was removed")
	}

	// Remove miss returns false.
	ok = reg.Remove("/home/user/project", "ws-999")
	if ok {
		t.Error("Remove returned true for nonexistent entry")
	}

	ok = reg.Remove("/nonexistent", "ws-2")
	if ok {
		t.Error("Remove returned true for nonexistent dir")
	}
}

