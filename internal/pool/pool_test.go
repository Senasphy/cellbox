package pool

import (
	"testing"

	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
)

func TestEligibleRequiresNetworkCompatibility(t *testing.T) {
	manager := &Manager{settings: config.Pool{Enabled: true, PerImage: 1, MemoryBudgetMB: warmMemoryMB}}
	base := runtime.SandboxSpec{Image: runtime.ImageInfo{Reference: "alpine:latest"}}
	tests := []struct {
		name string
		edit func(*runtime.SandboxSpec)
		want bool
	}{
		{name: "default no network", want: true},
		{name: "explicit no network", edit: func(spec *runtime.SandboxSpec) { spec.NetworkMode = "none" }, want: true},
		{name: "direct network mode", edit: func(spec *runtime.SandboxSpec) { spec.NetworkMode = "bridge" }},
		{name: "allowlist policy", edit: func(spec *runtime.SandboxSpec) { spec.NetworkPolicy.Mode = "allowlist" }},
		{name: "allow rules", edit: func(spec *runtime.SandboxSpec) {
			spec.NetworkPolicy.Allow = []runtime.EgressRule{{Host: "example.com", Ports: []int{443}}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			if test.edit != nil {
				test.edit(&spec)
			}
			if got := manager.eligible(spec); got != test.want {
				t.Fatalf("eligible() = %t, want %t", got, test.want)
			}
		})
	}
}
