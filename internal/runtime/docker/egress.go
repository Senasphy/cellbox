package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"

	"github.com/senasphy/cellbox/internal/egress"
	"github.com/senasphy/cellbox/internal/runtime"
)

const dockerEgressSocket = "/run/cellbox-egress.sock"
const dockerForwarderPath = "/tmp/cellbox"

type dockerEgress struct {
	dir    string
	proxy  *egress.Server
	cancel context.CancelFunc
	events *dockerEgressQueue
}

type dockerEgressQueue struct {
	mu     sync.Mutex
	open   bool
	events chan runtime.EgressEvent
}

func (e *dockerEgress) proxySocketPath() string { return path.Join(e.dir, "egress.sock") }

func newDockerEgressQueue() *dockerEgressQueue {
	return &dockerEgressQueue{open: true, events: make(chan runtime.EgressEvent, 256)}
}

func (q *dockerEgressQueue) publish(event runtime.EgressEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.open {
		q.events <- event
	}
}

func (q *dockerEgressQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.open {
		q.open = false
		close(q.events)
	}
}

func newDockerEgress(spec runtime.NetworkPolicy) (*dockerEgress, error) {
	policy := egress.Policy{Mode: spec.Mode, Allow: make([]egress.Rule, 0, len(spec.Allow)), ExtraBlockedCIDRs: spec.ExtraBlockedCIDRs, BandwidthBytesPerSec: spec.BandwidthBytesPerSec}
	for _, rule := range spec.Allow {
		policy.Allow = append(policy.Allow, egress.Rule{Host: rule.Host, Ports: rule.Ports})
	}
	if err := egress.ValidatePolicy(policy); err != nil {
		return nil, err
	}
	if policy.Mode == "" || policy.Mode == "none" {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "cellbox-egress-")
	if err != nil {
		return nil, err
	}
	queue := newDockerEgressQueue()
	proxy, err := egress.New(path.Join(dir, "egress.sock"), policy, func(event egress.Decision) {
		queue.publish(runtime.EgressEvent{Host: event.Host, Port: event.Port, Resolved: event.Resolved, Verdict: event.Verdict, Reason: event.Reason, BytesUp: event.BytesUp, BytesDown: event.BytesDown, Duration: event.Duration.Milliseconds(), Timestamp: event.Timestamp})
	})
	if err != nil {
		queue.close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := proxy.Start(ctx); err != nil {
		cancel()
		_ = proxy.Close()
		queue.close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	// The socket is mounted only into this sandbox. Its parent directory stays
	// private on the host, while the container's configured user can connect.
	if err := os.Chmod(path.Join(dir, "egress.sock"), 0666); err != nil {
		cancel()
		_ = proxy.Close()
		queue.close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &dockerEgress{dir: dir, proxy: proxy, cancel: cancel, events: queue}, nil
}

func (e *dockerEgress) env(target map[string]string) {
	if e == nil {
		return
	}
	for key, value := range map[string]string{
		"HTTP_PROXY":  "http://127.0.0.1:3128",
		"HTTPS_PROXY": "http://127.0.0.1:3128",
		"http_proxy":  "http://127.0.0.1:3128",
		"https_proxy": "http://127.0.0.1:3128",
		"NO_PROXY":    "localhost,127.0.0.1",
		"no_proxy":    "localhost,127.0.0.1",
	} {
		target[key] = value
	}
}

func (e *dockerEgress) close() {
	if e == nil {
		return
	}
	e.cancel()
	_ = e.proxy.Close()
	e.events.close()
	_ = os.RemoveAll(e.dir)
}

func egressHelperPath(sessionID string) (string, string, error) {
	if sessionID == "" {
		return "", "", fmt.Errorf("Docker egress requires a session id")
	}
	for _, char := range sessionID {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '_' && char != '-' {
			return "", "", fmt.Errorf("invalid session id for Docker egress helper")
		}
	}
	return "/.cellbox-forwarder-" + sessionID, ".cellbox-forwarder-" + sessionID, nil
}

func (r *Runtime) installEgressHelper(ctx context.Context, containerID, sessionID string) (string, error) {
	containerPath, archiveName, err := egressHelperPath(sessionID)
	if err != nil {
		return "", err
	}
	helper := r.helperPath
	if helper == "" {
		helper, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve cellbox egress helper executable: %w", err)
		}
	}
	file, err := os.Open(helper)
	if err != nil {
		return "", fmt.Errorf("open cellbox egress helper executable: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat cellbox egress helper executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
		return "", fmt.Errorf("cellbox egress helper size or type is invalid")
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: archiveName, Mode: 0755, Size: info.Size(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return "", fmt.Errorf("write egress helper archive header: %w", err)
	}
	if _, err := io.CopyN(tw, file, info.Size()); err != nil {
		return "", fmt.Errorf("write egress helper archive: %w", err)
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("close egress helper archive: %w", err)
	}
	response, err := r.requestReader(ctx, http.MethodPut, "/containers/"+url.PathEscape(containerID)+"/archive?path=%2F", "application/x-tar", bytes.NewReader(archive.Bytes()))
	if err != nil {
		return "", fmt.Errorf("copy egress helper into Docker container: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return "", fmt.Errorf("close Docker egress helper response: %w", err)
	}
	return containerPath, nil
}

func (s *sandbox) egressEnv(source map[string]string) []string {
	env := make(map[string]string, len(source)+6)
	for key, value := range source {
		env[key] = value
	}
	s.egress.env(env)
	result := make([]string, 0, len(env))
	for key, value := range env {
		result = append(result, key+"="+value)
	}
	return result
}
