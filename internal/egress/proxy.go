// Package egress implements the host side of the per-session HTTP proxy.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
)

type Rule struct {
	Host  string `json:"host"`
	Ports []int  `json:"ports"`
}

type Policy struct {
	Mode              string   `json:"mode"`
	Allow             []Rule   `json:"allow,omitempty"`
	ExtraBlockedCIDRs []string `json:"-"`
}

type Decision struct {
	Timestamp time.Time
	Host      string
	Port      int
	Resolved  string
	Verdict   string
	Reason    string
	BytesUp   int64
	BytesDown int64
	Duration  time.Duration
}

type Server struct {
	path     string
	policy   Policy
	blocked  []netip.Prefix
	log      func(Decision)
	listener *net.UnixListener
	http     *http.Server
	sem      chan struct{}
	close    sync.Once
	connMu   sync.Mutex
	closed   bool
	active   map[net.Conn]struct{}
	lookup   func(context.Context, string) ([]netip.Addr, error)
	dial     func(context.Context, string, string) (net.Conn, error)
}

func New(path string, policy Policy, logDecision func(Decision)) (*Server, error) {
	if err := ValidatePolicy(policy); err != nil {
		return nil, err
	}
	if policy.Mode == "" {
		policy.Mode = "none"
	}
	blocked := make([]netip.Prefix, 0, len(policy.ExtraBlockedCIDRs))
	for _, value := range policy.ExtraBlockedCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid blocked CIDR %q: %w", value, err)
		}
		blocked = append(blocked, prefix.Masked())
	}
	server := &Server{path: path, policy: policy, blocked: blocked, log: logDecision, sem: make(chan struct{}, 64), active: make(map[net.Conn]struct{})}
	server.lookup = resolve
	server.dial = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return server, nil
}

func ValidatePolicy(policy Policy) error {
	if policy.Mode == "" {
		policy.Mode = "none"
	}
	if policy.Mode != "none" && policy.Mode != "allowlist" && policy.Mode != "internet" {
		return fmt.Errorf("unsupported egress mode %q", policy.Mode)
	}
	if policy.Mode != "allowlist" && len(policy.Allow) > 0 {
		return errors.New("allow rules require network mode allowlist")
	}
	for _, rule := range policy.Allow {
		if _, err := normalizeRuleHost(rule.Host); err != nil {
			return fmt.Errorf("invalid egress allow host %q: %w", rule.Host, err)
		}
		if len(rule.Ports) == 0 {
			return fmt.Errorf("egress allow rule for %q must include ports", rule.Host)
		}
		for _, port := range rule.Ports {
			if port < 1 || port > 65535 {
				return fmt.Errorf("egress allow port %d is outside 1..65535", port)
			}
		}
	}
	for _, value := range policy.ExtraBlockedCIDRs {
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("invalid blocked CIDR %q: %w", value, err)
		}
	}
	return nil
}

func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create egress socket directory: %w", err)
	}
	if info, err := os.Lstat(s.path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket egress path %q", s.path)
		}
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("remove stale egress socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.path, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen on egress socket: %w", err)
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.path)
		return fmt.Errorf("protect egress socket: %w", err)
	}
	s.listener = listener
	s.http = &http.Server{Handler: http.HandlerFunc(s.serveHTTP), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	go func() { _ = s.http.Serve(listener) }()
	return nil
}

func (s *Server) Close() error {
	s.close.Do(func() {
		if s.http != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.http.Shutdown(ctx)
			cancel()
			_ = s.http.Close()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.connMu.Lock()
		s.closed = true
		for conn := range s.active {
			_ = conn.Close()
			delete(s.active, conn)
		}
		s.connMu.Unlock()
		_ = os.Remove(s.path)
	})
	return nil
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		http.Error(w, "too many proxy connections", http.StatusServiceUnavailable)
		return
	}
	started := time.Now()
	host, port, err := target(r)
	if err != nil {
		s.deny(w, r, started, "", 0, "", "invalid_target", http.StatusBadRequest)
		return
	}
	canonical, err := normalizeHost(host)
	if err != nil {
		s.deny(w, r, started, host, port, "", "invalid_host", http.StatusForbidden)
		return
	}
	if !s.allowed(canonical, port) {
		s.deny(w, r, started, canonical, port, "", "policy_denied", http.StatusForbidden)
		return
	}
	ips, err := s.lookup(r.Context(), canonical)
	if err != nil {
		s.deny(w, r, started, canonical, port, "", "dns_failed", http.StatusForbidden)
		return
	}
	for _, ip := range ips {
		if s.blockedIP(ip) {
			s.deny(w, r, started, canonical, port, ip.String(), "blocked_address", http.StatusForbidden)
			return
		}
	}
	remote := net.JoinHostPort(ips[0].String(), strconv.Itoa(port))
	conn, err := s.dial(r.Context(), "tcp", remote)
	if err != nil {
		s.deny(w, r, started, canonical, port, ips[0].String(), "connect_failed", http.StatusBadGateway)
		return
	}
	remoteAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok || !remoteAddr.AddrPort().Addr().Unmap().IsValid() || remoteAddr.AddrPort().Addr().Unmap() != ips[0].Unmap() || remoteAddr.Port != port {
		_ = conn.Close()
		s.deny(w, r, started, canonical, port, ips[0].String(), "remote_address_mismatch", http.StatusForbidden)
		return
	}
	var up, down int64
	if r.Method == http.MethodConnect {
		up, down = s.tunnel(w, r, conn)
	} else {
		up, down, err = s.forwardHTTP(w, r, conn, canonical, port)
	}
	_ = conn.Close()
	if s.log != nil {
		verdict, reason := "allow", "policy_allowed"
		if err != nil {
			verdict, reason = "deny", "upstream_failed"
		}
		s.log(Decision{Timestamp: time.Now().UTC(), Host: canonical, Port: port, Resolved: ips[0].String(), Verdict: verdict, Reason: reason, BytesUp: up, BytesDown: down, Duration: time.Since(started)})
	}
}

func target(r *http.Request) (string, int, error) {
	address := r.Host
	if r.Method == http.MethodConnect {
		address = r.Host
	} else if r.URL != nil && r.URL.IsAbs() {
		address = r.URL.Host
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") && r.Method != http.MethodConnect {
			host, portText = address, "80"
		} else {
			return "", 0, err
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("invalid port")
	}
	return strings.Trim(host, "[]"), port, nil
}

func normalizeHost(host string) (string, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", errors.New("zones are not allowed")
		}
		return ip.Unmap().String(), nil
	}
	if strings.ContainsAny(host, "%:/\\") || host == "" {
		return "", errors.New("invalid host syntax")
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if numericHostname(host) {
		return "", errors.New("ambiguous numeric address")
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || len(ascii) > 253 {
		return "", errors.New("invalid hostname")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid hostname")
		}
	}
	return ascii, nil
}

func numericHostname(host string) bool {
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" {
			return false
		}
		if strings.HasPrefix(label, "0x") {
			if len(label) == 2 {
				return false
			}
			for _, char := range label[2:] {
				if !strings.ContainsRune("0123456789abcdef", char) {
					return false
				}
			}
			continue
		}
		for _, char := range label {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func (s *Server) allowed(host string, port int) bool {
	switch s.policy.Mode {
	case "internet":
		return true
	case "allowlist":
		for _, rule := range s.policy.Allow {
			pattern, err := normalizeRuleHost(rule.Host)
			if err != nil || !hostMatches(pattern, host) {
				continue
			}
			for _, allowedPort := range rule.Ports {
				if allowedPort == port {
					return true
				}
			}
		}
	}
	return false
}

func normalizeRuleHost(host string) (string, error) {
	if strings.HasPrefix(host, "*.") {
		base, err := normalizeHost(host[2:])
		return "*." + base, err
	}
	return normalizeHost(host)
}

func hostMatches(pattern, host string) bool {
	if strings.HasPrefix(pattern, "*.") {
		base := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(host, "."+base) && host != base
	}
	return pattern == host
}

func resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("name did not resolve")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.Unmap())
	}
	return result, nil
}

var blockedNetworks = mustPrefixes("0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "255.0.0.0/8", "::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8")

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		out = append(out, netip.MustParsePrefix(value))
	}
	return out
}

func (s *Server) blockedIP(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() {
		return true
	}
	ip = ip.Unmap()
	for _, prefix := range blockedNetworks {
		if prefix.Contains(ip) {
			return true
		}
	}
	for _, prefix := range s.blocked {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) deny(w http.ResponseWriter, r *http.Request, started time.Time, host string, port int, resolved, reason string, status int) {
	w.Header().Set("X-Cellbox-Deny-Reason", reason)
	http.Error(w, http.StatusText(status), status)
	if s.log != nil {
		s.log(Decision{Timestamp: time.Now().UTC(), Host: host, Port: port, Resolved: resolved, Verdict: "deny", Reason: reason, Duration: time.Since(started)})
	}
}

func (s *Server) tunnel(w http.ResponseWriter, r *http.Request, upstream net.Conn) (int64, int64) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return 0, 0
	}
	client, rw, err := hijacker.Hijack()
	if err != nil {
		return 0, 0
	}
	if !s.track(client) {
		_ = client.Close()
		_ = upstream.Close()
		return 0, 0
	}
	if !s.track(upstream) {
		s.untrack(client)
		_ = client.Close()
		_ = upstream.Close()
		return 0, 0
	}
	defer s.untrack(client)
	defer s.untrack(upstream)
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Minute))
	_ = upstream.SetDeadline(time.Now().Add(2 * time.Minute))
	var up, down int64
	_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := rw.Flush(); err != nil {
		return 0, 0
	}
	clientReader := io.Reader(rw)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		up, _ = io.Copy(upstream, clientReader)
		if tcp, ok := upstream.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	down, _ = io.Copy(client, upstream)
	_ = client.SetDeadline(time.Now())
	wg.Wait()
	return up, down
}

func (s *Server) track(conn net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.closed {
		return false
	}
	s.active[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.connMu.Lock()
	delete(s.active, conn)
	s.connMu.Unlock()
}

func (s *Server) forwardHTTP(w http.ResponseWriter, r *http.Request, upstream net.Conn, host string, port int) (int64, int64, error) {
	if r.URL == nil || (r.URL.Scheme != "" && !strings.EqualFold(r.URL.Scheme, "http")) {
		http.Error(w, "plain HTTP forwarding requires an http URL", http.StatusBadRequest)
		return 0, 0, errors.New("unsupported forwarding scheme")
	}
	request := r.Clone(r.Context())
	request.RequestURI = ""
	request.URL.Scheme = "http"
	hostHeader := host
	if port != 80 {
		hostHeader = net.JoinHostPort(host, strconv.Itoa(port))
	}
	request.URL.Host = hostHeader
	request.Host = hostHeader
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	var body io.ReadCloser = request.Body
	if body == nil {
		body = http.NoBody
	}
	request.Body = &countingReader{Reader: body}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext:       func(context.Context, string, string) (net.Conn, error) { return upstream, nil },
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		transport.CloseIdleConnections()
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return request.Body.(*countingReader).N, 0, err
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	down, copyErr := io.Copy(w, response.Body)
	transport.CloseIdleConnections()
	return request.Body.(*countingReader).N, down, copyErr
}

type countingReader struct {
	Reader io.ReadCloser
	N      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.N += int64(n)
	return n, err
}

func (r *countingReader) Close() error { return r.Reader.Close() }
