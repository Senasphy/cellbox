package api

import (
	"context"
	"testing"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

func TestCollectSessionResourceSamples(t *testing.T) {
	backend := &fakeRuntime{boxes: map[string]*fakeSandbox{"container-stream": {id: "container-stream", stats: runtime.Stats{CPUUsageNS: 1234, MemoryBytes: 5678}}}}
	records := []store.SessionRecord{
		{ID: "ses_ready", RuntimeID: "container-stream", State: "ready"},
		{ID: "ses_stopped", RuntimeID: "container-stream", State: "stopped"},
		{ID: "ses_missing_runtime", State: "ready"},
	}
	samples := collectSessionResourceSamples(context.Background(), backend, records)
	if got := samples["ses_ready"]; got.CPUUsageNS != 1234 || got.MemoryBytes != 5678 || got.SampledAt.IsZero() {
		t.Fatalf("ready session sample=%+v", got)
	}
	if _, ok := samples["ses_stopped"]; ok {
		t.Fatal("stopped session unexpectedly received a resource sample")
	}
	if _, ok := samples["ses_missing_runtime"]; ok {
		t.Fatal("session without a runtime id unexpectedly received a resource sample")
	}
}
