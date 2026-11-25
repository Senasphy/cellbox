// Command load runs concurrent session create/exec/destroy cycles against a
// cellbox server, reporting latency percentiles and optional RSS/FD/cgroup leak
// checks for soak testing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cellbox "github.com/senasphy/cellbox/sdk/go"
)

type summary struct {
	Image         string  `json:"image"`
	Concurrency   int     `json:"concurrency"`
	DurationMS    int64   `json:"duration_ms"`
	Samples       int     `json:"samples"`
	Errors        int64   `json:"errors"`
	CreateP50MS   float64 `json:"create_p50_ms"`
	CreateP95MS   float64 `json:"create_p95_ms"`
	CreateP99MS   float64 `json:"create_p99_ms"`
	ExecP50MS     float64 `json:"exec_p50_ms"`
	ExecP95MS     float64 `json:"exec_p95_ms"`
	ExecP99MS     float64 `json:"exec_p99_ms"`
	RSSBeforeKB   int64   `json:"rss_before_kb,omitempty"`
	RSSAfterKB    int64   `json:"rss_after_kb,omitempty"`
	FDsBefore     int     `json:"fds_before,omitempty"`
	FDsAfter      int     `json:"fds_after,omitempty"`
	LeakedCgroups int     `json:"leaked_cgroups,omitempty"`
}

func main() {
	var (
		baseURL     = flag.String("url", envOr("CELLBOX_URL", "http://127.0.0.1:8080"), "cellbox service URL")
		apiKey      = flag.String("key", os.Getenv("CELLBOX_KEY"), "cellbox bearer API key")
		image       = flag.String("image", "alpine:latest", "sandbox image")
		concurrency = flag.Int("concurrency", 4, "concurrent worker count")
		duration    = flag.Duration("duration", 10*time.Second, "load or soak duration")
		serverPID   = flag.Int("pid", 0, "optional server PID to watch for RSS and FD growth")
		cgroupDir   = flag.String("cgroup", "", "optional delegated sessions cgroup directory to check for leaks")
		maxRSSMB    = flag.Int64("max-rss-growth-mb", 64, "maximum allowed server RSS growth in MiB")
		maxFDGrowth = flag.Int("max-fd-growth", 16, "maximum allowed server FD growth")
	)
	flag.Parse()
	if *apiKey == "" || *concurrency < 1 || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "usage: load -key <api-key> [-url ...] [-concurrency 4] [-duration 10s]")
		os.Exit(2)
	}

	var rssBeforeKB, rssAfterKB int64
	var fdsBefore, fdsAfter int
	var err error
	if *serverPID > 0 {
		if rssBeforeKB, fdsBefore, err = sampleProc(*serverPID); err != nil {
			fmt.Fprintf(os.Stderr, "sample initial process stats: %v\n", err)
			os.Exit(1)
		}
	}

	client := &cellbox.Client{
		BaseURL: *baseURL,
		APIKey:  *apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	var (
		mu              sync.Mutex
		createLatencies []float64
		execLatencies   []float64
		errCount        atomic.Int64
		lastErr         atomic.Value
		wg              sync.WaitGroup
	)

	started := time.Now()
	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				cStart := time.Now()
				session, createErr := client.CreateSession(ctx, *image, map[string]int{
					"cpu_millicores": 500,
					"memory_mb":      128,
					"pids":           32,
				})
				if createErr != nil {
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						return
					}
					errCount.Add(1)
					lastErr.Store(createErr.Error())
					continue
				}
				cElapsed := float64(time.Since(cStart).Microseconds()) / 1000.0

				eStart := time.Now()
				_, execErr := client.Exec(ctx, session.ID, []string{"/bin/echo", "ok"}, 15*time.Second)
				eElapsed := float64(time.Since(eStart).Microseconds()) / 1000.0

				delCtx, delCancel := context.WithTimeout(context.Background(), 15*time.Second)
				delErr := client.DeleteSession(delCtx, session.ID)
				delCancel()

				if execErr != nil {
					if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
						errCount.Add(1)
						lastErr.Store(execErr.Error())
					}
					continue
				}
				if delErr != nil {
					errCount.Add(1)
					lastErr.Store(delErr.Error())
					continue
				}
				mu.Lock()
				createLatencies = append(createLatencies, cElapsed)
				execLatencies = append(execLatencies, eElapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(started)

	leakedCgroups := 0
	if *cgroupDir != "" {
		leakedCgroups, err = countSubdirs(*cgroupDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "inspect cgroup dir: %v\n", err)
			os.Exit(1)
		}
	}
	if *serverPID > 0 {
		if rssAfterKB, fdsAfter, err = sampleProc(*serverPID); err != nil {
			fmt.Fprintf(os.Stderr, "sample final process stats: %v\n", err)
			os.Exit(1)
		}
	}

	out := summary{
		Image:         *image,
		Concurrency:   *concurrency,
		DurationMS:    elapsed.Milliseconds(),
		Samples:       len(createLatencies),
		Errors:        errCount.Load(),
		CreateP50MS:   percentile(createLatencies, 0.50),
		CreateP95MS:   percentile(createLatencies, 0.95),
		CreateP99MS:   percentile(createLatencies, 0.99),
		ExecP50MS:     percentile(execLatencies, 0.50),
		ExecP95MS:     percentile(execLatencies, 0.95),
		ExecP99MS:     percentile(execLatencies, 0.99),
		RSSBeforeKB:   rssBeforeKB,
		RSSAfterKB:    rssAfterKB,
		FDsBefore:     fdsBefore,
		FDsAfter:      fdsAfter,
		LeakedCgroups: leakedCgroups,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))

	failed := false
	if out.Samples == 0 || out.Errors > 0 {
		if msg, ok := lastErr.Load().(string); ok {
			fmt.Fprintf(os.Stderr, "load errors=%d last=%s\n", out.Errors, msg)
		}
		failed = true
	}
	if *serverPID > 0 {
		if rssAfterKB-rssBeforeKB > *maxRSSMB*1024 {
			fmt.Fprintf(os.Stderr, "server RSS grew by %d KiB (limit %d MiB)\n", rssAfterKB-rssBeforeKB, *maxRSSMB)
			failed = true
		}
		if fdsAfter-fdsBefore > *maxFDGrowth {
			fmt.Fprintf(os.Stderr, "server FD count grew by %d (limit %d)\n", fdsAfter-fdsBefore, *maxFDGrowth)
			failed = true
		}
	}
	if leakedCgroups > 0 {
		fmt.Fprintf(os.Stderr, "leaked %d child cgroups in %s\n", leakedCgroups, *cgroupDir)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	idx := int(q*float64(len(sorted))+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func sampleProc(pid int) (int64, int, error) {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	var rssKB int64
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				rssKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
			break
		}
	}
	fdDir := filepath.Join(fmt.Sprintf("/proc/%d", pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return 0, 0, err
	}
	return rssKB, len(entries), nil
}

func countSubdirs(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count, nil
}
