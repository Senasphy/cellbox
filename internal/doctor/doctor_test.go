package doctor

import "testing"

func TestAtLeastKernel(t *testing.T) {
	for _, test := range []struct {
		release string
		want    bool
	}{
		{"5.14.0-arch1", true},
		{"6.1.0", true},
		{"5.13.99", false},
		{"5.x", false},
		{"", false},
	} {
		if got := atLeastKernel(test.release, 5, 14); got != test.want {
			t.Errorf("atLeastKernel(%q) = %v, want %v", test.release, got, test.want)
		}
	}
}

func TestMissingFields(t *testing.T) {
	have := fieldsSet("cpu memory io")
	got := missingFields(have, []string{"cpu", "memory", "pids"})
	if len(got) != 1 || got[0] != "pids" {
		t.Fatalf("missingFields = %v, want [pids]", got)
	}
}
