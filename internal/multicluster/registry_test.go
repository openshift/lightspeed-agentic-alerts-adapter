package multicluster

import (
	"testing"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
)

func TestRegistrySnapshots(t *testing.T) {
	local := []adapter.Target{{Name: "local"}}
	registry := NewRegistry(local, []adapter.Target{{Name: "spoke-a"}})

	local[0].Name = "changed-local"
	first := registry.Targets()
	if !targetNames(first)["local"] {
		t.Errorf("initial snapshot = %v, want local target", targetNames(first))
	}

	registry.SetSpoke(adapter.Target{Name: "spoke-b"})
	registry.RemoveSpoke("spoke-a")

	if !targetNames(first)["spoke-a"] {
		t.Errorf("initial snapshot = %v, want spoke-a target", targetNames(first))
	}

	second := registry.Targets()
	names := targetNames(second)
	if !names["local"] || !names["spoke-b"] || names["spoke-a"] {
		t.Errorf("updated snapshot = %v, want local and spoke-b only", names)
	}

	for i := range second {
		if second[i].Name == "local" {
			second[i].Name = "changed-local"
		}
	}
	if !targetNames(registry.Targets())["local"] {
		t.Errorf("registry targets changed after snapshot mutation")
	}
}

func targetNames(targets []adapter.Target) map[string]bool {
	names := make(map[string]bool, len(targets))
	for _, target := range targets {
		names[target.Name] = true
	}
	return names
}
