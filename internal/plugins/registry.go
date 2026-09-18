package plugins

import "sync"

// entry is a compiled plugin plus the version it was compiled under.
type entry struct {
	version string
	prog    *Program
}

// Registry holds exactly one compiled plugin per mod.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]entry{}}
}

// Put compiles source and stores it under modID, replacing any existing entry.
func (r *Registry) Put(modID, version, source string) error {
	prog, err := Compile(modID, source)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[modID] = entry{version: version, prog: prog}
	return nil
}

// Get returns the program for modID, provided it is stored under exactly this version.
func (r *Registry) Get(modID, version string) (*Program, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[modID]
	if !ok || e.version != version {
		return nil, false
	}
	return e.prog, true
}

// Drop removes modID's program, e.g.
func (r *Registry) Drop(modID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, modID)
}
