package egress

import (
	"net/http"
	"net/url"
	"testing"
)

func FuzzProxyTargetAndHost(f *testing.F) {
	f.Add(http.MethodConnect, "example.com:443", "", "allowlist", "example.com", 443)
	f.Add(http.MethodGet, "127.0.0.1:8080", "http://169.254.169.254/latest/meta-data/", "internet", "*.github.com", 443)
	f.Add(http.MethodGet, "[::ffff:127.0.0.1]:80", "http://[::1]:8080/", "allowlist", "0x7f000001", 80)
	f.Add(http.MethodGet, "", "ftp://example.com/file", "none", "bad host", 0)

	f.Fuzz(func(t *testing.T, method, hostHeader, rawURL, mode, ruleHost string, port int) {
		req := &http.Request{Method: method, Host: hostHeader}
		if parsed, err := url.Parse(rawURL); err == nil {
			req.URL = parsed
		}
		if parsedHost, parsedPort, err := target(req); err == nil {
			if parsedPort < 1 || parsedPort > 65535 {
				t.Fatalf("target returned out-of-range port %d", parsedPort)
			}
			_, _ = normalizeHost(parsedHost)
		}
		_, _ = normalizeHost(hostHeader)
		_ = ValidatePolicy(Policy{
			Mode:  mode,
			Allow: []Rule{{Host: ruleHost, Ports: []int{port}}},
		})
	})
}
