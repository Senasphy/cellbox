package doctor

import (
	"fmt"
	"os"
	"runtime"
)

// Check performs safe baseline host checks; native-runtime checks are added with that phase.
func Check() []string {
	var out []string
	out = append(out, fmt.Sprintf("platform: %s/%s", runtime.GOOS, runtime.GOARCH))
	if runtime.GOOS != "linux" {
		out = append(out, "native backend requires Linux (fix: run cellbox inside a Linux VM)")
		return out
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		out = append(out, "cgroup v2: unavailable (fix: mount a unified cgroup v2 hierarchy)")
	} else {
		out = append(out, "cgroup v2: present (delegation checks are not yet implemented)")
	}
	return out
}
