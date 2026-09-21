package multicluster

import (
	"sync"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
)

// Registry combines fixed local targets with dynamically managed spoke targets.
type Registry struct {
	mu     sync.RWMutex
	local  []adapter.Target
	spokes map[string]adapter.Target
}

// NewRegistry creates a target registry with fixed local and initial spoke
// targets.
func NewRegistry(local, spokes []adapter.Target) *Registry {
	r := &Registry{
		local:  append([]adapter.Target(nil), local...),
		spokes: make(map[string]adapter.Target, len(spokes)),
	}
	for _, target := range spokes {
		r.spokes[target.Name] = target
	}
	return r
}

// SetSpoke stores target under its SpokeCluster name and reports whether it was
// newly added.
func (r *Registry) SetSpoke(target adapter.Target) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.spokes[target.Name]
	r.spokes[target.Name] = target
	return !exists
}

// RemoveSpoke removes the target for a SpokeCluster name and reports whether
// one was removed.
func (r *Registry) RemoveSpoke(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.spokes[name]
	delete(r.spokes, name)
	return exists
}

// Targets returns a point-in-time copy of the local and spoke targets.
func (r *Registry) Targets() []adapter.Target {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targets := make([]adapter.Target, 0, len(r.local)+len(r.spokes))
	targets = append(targets, r.local...)
	for _, target := range r.spokes {
		targets = append(targets, target)
	}
	return targets
}
