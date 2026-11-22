//go:build linux

package native

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// helperChildren tracks direct helper processes whose exit status belongs to
// os/exec. PID 1 must reap adopted workload descendants without consuming the
// status of one of these helpers.
var helperChildren = struct {
	sync.Mutex
	pids map[int]struct{}
}{pids: make(map[int]struct{})}

func startTrackedHelper(command *exec.Cmd) error {
	helperChildren.Lock()
	defer helperChildren.Unlock()
	if err := command.Start(); err != nil {
		return err
	}
	helperChildren.pids[command.Process.Pid] = struct{}{}
	return nil
}

func waitTrackedHelper(command *exec.Cmd) error {
	err := command.Wait()
	helperChildren.Lock()
	delete(helperChildren.pids, command.Process.Pid)
	helperChildren.Unlock()
	return err
}

func trackedHelper(pid int) bool {
	helperChildren.Lock()
	_, found := helperChildren.pids[pid]
	helperChildren.Unlock()
	return found
}

// startOrphanReaper runs only in the sandbox's PID namespace init. It waits
// only for adopted zombie processes, leaving live processes and tracked
// os/exec children untouched.
func startOrphanReaper() func() {
	children := make(chan os.Signal, 1)
	signal.Notify(children, syscall.SIGCHLD)
	done := make(chan struct{})
	go func() {
		defer signal.Stop(children)
		for {
			select {
			case <-children:
				reapAdoptedZombies()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func reapAdoptedZombies() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || trackedHelper(pid) {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil || !isAdoptedZombie(stat) {
			continue
		}
		var status unix.WaitStatus
		_, _ = unix.Wait4(pid, &status, unix.WNOHANG, nil)
	}
}

func isAdoptedZombie(stat []byte) bool {
	// comm is parenthesized and may itself contain spaces or ')'. Fields after
	// its final ')' begin with state (field 3), then ppid (field 4).
	close := strings.LastIndexByte(string(stat), ')')
	if close < 0 || close+1 >= len(stat) {
		return false
	}
	fields := strings.Fields(string(stat[close+1:]))
	if len(fields) < 2 || fields[0] != "Z" {
		return false
	}
	parent, err := strconv.Atoi(fields[1])
	return err == nil && parent == 1
}
