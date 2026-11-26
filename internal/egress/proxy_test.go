package egress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPolicyWildcardAndPortMatching(t *testing.T) {
	server, err := New(t.TempDir()+"/proxy.sock", Policy{Mode: "allowlist", Allow: []Rule{{Host: "*.example.com", Ports: []int{443}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host string
		port int
		want bool
	}{{"api.example.com", 443, true}, {"example.com", 443, false}, {"api.example.com", 80, false}} {
		if got := server.allowed(test.host, test.port); got != test.want {
			t.Errorf("allowed(%q,%d)=%v want %v", test.host, test.port, got, test.want)
		}
	}
}

func TestTargetDefaultsPortsAndParsesIPv6(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		target      string
		requestHost string
		wantHost    string
		wantPort    int
		wantErr     bool
	}{
		{name: "IPv4 HTTP default", method: http.MethodGet, target: "http://example.test/path", wantHost: "example.test", wantPort: 80},
		{name: "IPv6 HTTP default", method: http.MethodGet, target: "http://[2606:4700::4700]/path", wantHost: "2606:4700::4700", wantPort: 80},
		{name: "IPv6 HTTPS default", method: http.MethodGet, target: "https://[2606:4700::4700]/path", wantHost: "2606:4700::4700", wantPort: 443},
		{name: "IPv6 CONNECT explicit port", method: http.MethodConnect, target: "http://example.test", requestHost: "[2606:4700::4700]:443", wantHost: "2606:4700::4700", wantPort: 443},
		{name: "CONNECT requires port", method: http.MethodConnect, target: "http://example.test", requestHost: "example.test", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			if test.requestHost != "" {
				request.Host = test.requestHost
			}
			host, port, err := target(request)
			if test.wantErr {
				if err == nil {
					t.Fatal("target() error = nil, want error")
				}
				return
			}
			if err != nil || host != test.wantHost || port != test.wantPort {
				t.Fatalf("target() = (%q, %d, %v), want (%q, %d, nil)", host, port, err, test.wantHost, test.wantPort)
			}
		})
	}
}

func TestUnixProxyForwardsAllowedHTTPAndDeniesPrivateIP(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upstreamServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "hello")
	})}
	go func() { _ = upstreamServer.Serve(upstream) }()
	t.Cleanup(func() { _ = upstreamServer.Close() })
	port := upstream.Addr().(*net.TCPAddr).Port
	var decisions []Decision
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "allowlist", Allow: []Rule{{Host: "upstream.test", Ports: []int{port}}}}, func(event Decision) {
		decisions = append(decisions, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	proxy.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		if err != nil {
			return nil, err
		}
		return reportedRemoteConn{Conn: conn, addr: &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: port}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", proxy.path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://upstream.test:%d/test", port))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("forward response status=%d body=%q err=%v", response.StatusCode, body, err)
	}
	redirectResponse, err := client.Get(fmt.Sprintf("http://upstream.test:%d/redirect", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = redirectResponse.Body.Close()
	if redirectResponse.StatusCode != http.StatusForbidden || redirectResponse.Header.Get("X-Cellbox-Deny-Reason") != "policy_denied" {
		t.Fatalf("private redirect status=%d reason=%q", redirectResponse.StatusCode, redirectResponse.Header.Get("X-Cellbox-Deny-Reason"))
	}
	privateURL, _ := url.Parse("http://127.0.0.1:80/")
	request := &http.Request{Method: http.MethodGet, URL: privateURL, Host: "127.0.0.1:80", Header: make(http.Header)}
	result, err := proxyRequestOverUnix(client, request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusForbidden || result.Header.Get("X-Cellbox-Deny-Reason") != "policy_denied" {
		t.Fatalf("private address status=%d reason=%q", result.StatusCode, result.Header.Get("X-Cellbox-Deny-Reason"))
	}
	if len(decisions) != 4 || decisions[0].Verdict != "allow" || decisions[1].Verdict != "allow" || decisions[2].Verdict != "deny" || decisions[3].Verdict != "deny" {
		t.Fatalf("proxy decisions=%+v", decisions)
	}
}

type reportedRemoteConn struct {
	net.Conn
	addr net.Addr
}

func (c reportedRemoteConn) RemoteAddr() net.Addr { return c.addr }

func proxyRequestOverUnix(client *http.Client, request *http.Request) (*http.Response, error) {
	request.URL.Scheme = "http"
	return client.Do(request)
}

func TestUnixProxyBoundsHeaderSizeAndSlowHeaderReads(t *testing.T) {
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "none"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	dial := func() net.Conn {
		conn, err := net.DialTimeout("unix", proxy.path, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(7 * time.Second))
		return conn
	}
	large := dial()
	_, _ = io.WriteString(large, "GET http://example.test/ HTTP/1.1\r\nHost: example.test\r\nX-Large: "+strings.Repeat("a", 20<<10)+"\r\n\r\n")
	largeResponse, err := io.ReadAll(large)
	_ = large.Close()
	if err != nil || !bytes.Contains(largeResponse, []byte("431")) {
		t.Fatalf("oversized header response=%q err=%v", largeResponse, err)
	}
	slow := dial()
	_, _ = io.WriteString(slow, "GET / HTTP/1.1\r\nHost: example.test\r\nX-Slow: ")
	started := time.Now()
	slowResponse, err := io.ReadAll(slow)
	_ = slow.Close()
	if err != nil || time.Since(started) > 7*time.Second {
		t.Fatalf("slow header response err=%v elapsed=%s", err, time.Since(started))
	}
	if len(slowResponse) != 0 {
		t.Fatalf("slow header unexpectedly received a response: %q", slowResponse)
	}
}

func TestNormalizeHostRejectsAmbiguousNumericAndCanonicalizesIDNA(t *testing.T) {
	for _, host := range []string{"2130706433", "0177.0.0.1", "0x7f.0.0.1"} {
		if _, err := normalizeHost(host); err == nil {
			t.Errorf("accepted ambiguous numeric host %q", host)
		}
	}
	got, err := normalizeHost("BÜCHER.example.")
	if err != nil || got != "xn--bcher-kva.example" {
		t.Fatalf("normalized host=%q err=%v", got, err)
	}
}

func TestBlockedAddressesIncludeMappedAndConfiguredCIDRs(t *testing.T) {
	server, err := New(t.TempDir()+"/proxy.sock", Policy{Mode: "internet", ExtraBlockedCIDRs: []string{"203.0.113.0/24"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{
		"0.0.0.0", "10.1.2.3", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.0.1",
		"192.0.0.1", "192.0.2.1", "192.168.0.1", "198.18.0.1", "198.51.100.1", "203.0.113.8",
		"224.0.0.1", "240.0.0.1", "255.255.255.255", "::", "::1", "64:ff9b::a00:1", "64:ff9b:1::a00:1",
		"100::1", "2001::1", "2001:2::1", "2001:db8::1", "2002:c000:0201::1", "3fff::1", "fc00::1", "fe80::1", "ff02::1",
		"::ffff:10.1.2.3", "::ffff:172.16.0.1", "::ffff:192.168.0.1", "::ffff:169.254.0.1",
	} {
		ip := netip.MustParseAddr(address)
		if !server.blockedIP(ip) {
			t.Errorf("address %s was not blocked", ip)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if server.blockedIP(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was blocked", address)
		}
	}
}

func TestProxyRejectsMixedPublicPrivateDNSAnswerBeforeDial(t *testing.T) {
	var decisions []Decision
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "internet"}, func(event Decision) {
		decisions = append(decisions, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.5")}, nil
	}
	dialed := false
	proxy.dial = func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, fmt.Errorf("unexpected dial")
	}
	request := httptest.NewRequest(http.MethodGet, "http://rebinding.example/", nil)
	request.Host = "rebinding.example:80"
	response := httptest.NewRecorder()
	proxy.serveHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("X-Cellbox-Deny-Reason") != "blocked_address" {
		t.Fatalf("status=%d reason=%q", response.Code, response.Header().Get("X-Cellbox-Deny-Reason"))
	}
	if dialed || len(decisions) != 1 || decisions[0].Verdict != "deny" {
		t.Fatalf("dialed=%v decisions=%+v", dialed, decisions)
	}
}

func TestProxyPinsAddressWhenDNSAnswerChangesBetweenLookups(t *testing.T) {
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "internet"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lookups := 0
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}
	dialTarget := ""
	proxy.dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dialTarget = address
		return nil, fmt.Errorf("stop after validating dial target")
	}
	request := httptest.NewRequest(http.MethodGet, "http://rebinding.example/", nil)
	response := httptest.NewRecorder()
	proxy.serveHTTP(response, request)
	if lookups != 1 || dialTarget != "8.8.8.8:80" {
		t.Fatalf("DNS lookups=%d dial target=%q, want one lookup pinned to 8.8.8.8:80", lookups, dialTarget)
	}
	if response.Code != http.StatusBadGateway || response.Header().Get("X-Cellbox-Deny-Reason") != "connect_failed" {
		t.Fatalf("status=%d deny reason=%q", response.Code, response.Header().Get("X-Cellbox-Deny-Reason"))
	}
}

func TestProxyRejectsConnectedPeerDifferentFromValidatedIP(t *testing.T) {
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "internet"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	client, peer := net.Pipe()
	defer peer.Close()
	proxy.dial = func(context.Context, string, string) (net.Conn, error) {
		return reportedRemoteConn{Conn: client, addr: &net.TCPAddr{IP: net.ParseIP("1.1.1.1"), Port: 80}}, nil
	}
	request := httptest.NewRequest(http.MethodGet, "http://peer-check.example/", nil)
	request.Host = "peer-check.example:80"
	response := httptest.NewRecorder()
	proxy.serveHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("X-Cellbox-Deny-Reason") != "remote_address_mismatch" {
		t.Fatalf("status=%d reason=%q", response.Code, response.Header().Get("X-Cellbox-Deny-Reason"))
	}
	if _, err := client.Write([]byte("x")); err == nil {
		t.Fatal("mismatched peer connection was not closed")
	}
}

func TestProxyRejectsDisallowedPortBeforeResolution(t *testing.T) {
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "allowlist", Allow: []Rule{{Host: "example.test", Ports: []int{443}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := false
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		resolved = true
		return nil, fmt.Errorf("unexpected DNS lookup")
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test:8080/", nil)
	response := httptest.NewRecorder()
	proxy.serveHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("X-Cellbox-Deny-Reason") != "policy_denied" {
		t.Fatalf("status=%d reason=%q", response.Code, response.Header().Get("X-Cellbox-Deny-Reason"))
	}
	if resolved {
		t.Fatal("proxy resolved a host before rejecting its disallowed port")
	}
}

func TestPolicyValidationRejectsOpenOrMalformedRules(t *testing.T) {
	for _, policy := range []Policy{
		{Mode: "none", Allow: []Rule{{Host: "example.com", Ports: []int{443}}}},
		{Mode: "allowlist", Allow: []Rule{{Host: "example.com", Ports: []int{0}}}},
		{Mode: "allowlist", Allow: []Rule{{Host: "*.", Ports: []int{443}}}},
		{Mode: "none", BandwidthBytesPerSec: -1},
	} {
		if err := ValidatePolicy(policy); err == nil {
			t.Errorf("accepted invalid policy: %+v", policy)
		}
	}
}

func TestProxyEnforcesBandwidthCap(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 12<<10)
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upstreamServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})}
	go func() { _ = upstreamServer.Serve(upstream) }()
	t.Cleanup(func() { _ = upstreamServer.Close() })
	port := upstream.Addr().(*net.TCPAddr).Port
	proxy, err := New(filepath.Join(t.TempDir(), "proxy.sock"), Policy{Mode: "allowlist", Allow: []Rule{{Host: "upstream.test", Ports: []int{port}}}, BandwidthBytesPerSec: 16 << 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	proxy.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		if err != nil {
			return nil, err
		}
		return reportedRemoteConn{Conn: conn, addr: &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: port}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", proxy.path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	started := time.Now()
	response, err := client.Get(fmt.Sprintf("http://upstream.test:%d/blob", port))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	elapsed := time.Since(started)
	if err != nil || !bytes.Equal(body, payload) {
		t.Fatalf("len=%d err=%v", len(body), err)
	}
	if elapsed < 350*time.Millisecond {
		t.Fatalf("bandwidth cap did not throttle response: elapsed=%v", elapsed)
	}
}
