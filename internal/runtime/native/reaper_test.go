//go:build linux

package native

import "testing"

func TestIsAdoptedZombie(t *testing.T) {
	tests := []struct {
		name string
		stat string
		want bool
	}{
		{name: "adopted zombie with parenthesis in comm", stat: "42 (worker ) child) Z 1 0 0", want: true},
		{name: "running child", stat: "42 (worker) S 1 0 0", want: false},
		{name: "zombie with another parent", stat: "42 (worker) Z 27 0 0", want: false},
		{name: "malformed stat", stat: "42 (worker)", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isAdoptedZombie([]byte(test.stat)); got != test.want {
				t.Fatalf("isAdoptedZombie(%q) = %t, want %t", test.stat, got, test.want)
			}
		})
	}
}
