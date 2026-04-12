package provider

import (
	"net/http"
	"testing"
)

type stubProvider struct{ kind string }

func (s *stubProvider) Kind() string { return s.kind }
func (s *stubProvider) ParseUpload(string, []byte) (*UploadResult, error) {
	return nil, nil
}
func (s *stubProvider) BuildSandboxConfig([]*BindingMeta, string, string) ([]*SandboxConfigFile, error) {
	return nil, nil
}
func (s *stubProvider) ServeHTTP(http.ResponseWriter, *http.Request, *DecryptedBinding) {}

func TestRegisterAndLookup(t *testing.T) {
	Reset()
	defer Reset()

	Register("test", &stubProvider{kind: "test"})

	p, err := Lookup("test")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.Kind() != "test" {
		t.Fatalf("got kind %q, want %q", p.Kind(), "test")
	}
}

func TestLookupUnknown(t *testing.T) {
	Reset()
	defer Reset()

	_, err := Lookup("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDuplicateRegisterPanics(t *testing.T) {
	Reset()
	defer Reset()

	Register("dup", &stubProvider{kind: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register("dup", &stubProvider{kind: "dup"})
}

func TestAllSorted(t *testing.T) {
	Reset()
	defer Reset()

	Register("z-provider", &stubProvider{kind: "z-provider"})
	Register("a-provider", &stubProvider{kind: "a-provider"})
	Register("m-provider", &stubProvider{kind: "m-provider"})

	all := All()
	if len(all) != 3 {
		t.Fatalf("got %d providers, want 3", len(all))
	}
	if all[0].Kind() != "a-provider" || all[1].Kind() != "m-provider" || all[2].Kind() != "z-provider" {
		t.Fatalf("wrong order: %s, %s, %s", all[0].Kind(), all[1].Kind(), all[2].Kind())
	}
}
