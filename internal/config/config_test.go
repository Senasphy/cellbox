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
