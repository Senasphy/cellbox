package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestLoadStrictYAMLAndEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellbox.yaml")
	if err := os.WriteFile(p, []byte("listen: 127.0.0.1:9090\nunknown: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected strict unknown-field error")
	}
	if err := os.WriteFile(p, []byte("listen: 127.0.0.1:9090\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELLBOX_LISTEN", "127.0.0.1:7070")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:7070" {
		t.Fatalf("listen=%s", c.Listen)
	}
}
func TestValidateTLSPair(t *testing.T) {
	c := Defaults()
	c.TLS.Cert = "cert"
	if err := c.Validate(); err == nil {
		t.Fatal("expected unmatched certificate error")
	}
}

func TestValidateNetworkAndLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"invalid listen port", func(c *Config) { c.Listen = "127.0.0.1:70000" }},
		{"invalid CIDR", func(c *Config) { c.Egress.ExtraBlockedCIDRs = []string{"metadata"} }},
		{"negative bandwidth cap", func(c *Config) { c.Egress.BandwidthBytesPerSec = -1 }},
		{"zero terminal grace", func(c *Config) { c.Terminal.GraceS = 0 }},
		{"excessive terminal grace", func(c *Config) { c.Terminal.GraceS = 3601 }},
		{"max below default", func(c *Config) { c.Limits.Max.MemoryMB = 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Defaults()
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEnvOverridesForTerminalAndEgress(t *testing.T) {
	t.Setenv("CELLBOX_TERMINAL_GRACE_S", "45")
	t.Setenv("CELLBOX_EGRESS_BANDWIDTH_BPS", "1048576")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Terminal.GraceS != 45 {
		t.Fatalf("terminal.grace_s=%d", c.Terminal.GraceS)
	}
	if c.Egress.BandwidthBytesPerSec != 1048576 {
		t.Fatalf("egress.bandwidth_bytes_per_sec=%d", c.Egress.BandwidthBytesPerSec)
	}
}
