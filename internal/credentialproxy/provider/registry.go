package provider

import (
	"fmt"
	"sort"
	"sync"
)

var global = &Registry{
	providers: make(map[string]Provider),
}

// Registry holds registered credential providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// Register adds a provider to the global registry. Panics on duplicate kind.
func Register(kind string, p Provider) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if _, exists := global.providers[kind]; exists {
		panic(fmt.Sprintf("credentialproxy: duplicate provider kind %q", kind))
	}
	global.providers[kind] = p
}

// Lookup returns the provider for the given kind.
func Lookup(kind string) (Provider, error) {
	global.mu.RLock()
	defer global.mu.RUnlock()
	p, ok := global.providers[kind]
	if !ok {
		return nil, fmt.Errorf("unknown credential provider kind %q", kind)
	}
	return p, nil
}

// All returns all registered providers sorted by kind.
func All() []Provider {
	global.mu.RLock()
	defer global.mu.RUnlock()
	result := make([]Provider, 0, len(global.providers))
	for _, p := range global.providers {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Kind() < result[j].Kind()
	})
	return result
}

// Reset clears the registry. For testing only.
func Reset() {
	global.mu.Lock()
	defer global.mu.Unlock()
	global.providers = make(map[string]Provider)
}
