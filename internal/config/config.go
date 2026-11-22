package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the service configuration.
type Config struct {
	Listen       string    `yaml:"listen"`
	DataDir      string    `yaml:"data_dir"`
	Backend      string    `yaml:"backend"`
	DockerSocket string    `yaml:"docker_socket"`
	Limits       Limits    `yaml:"limits"`
	Capacity     Capacity  `yaml:"capacity"`
	Pool         Pool      `yaml:"pool"`
	Egress       Egress    `yaml:"egress"`
	Retention    Retention `yaml:"retention"`
	Log          Log       `yaml:"log"`
	TLS          TLS       `yaml:"tls"`
}
type Limits struct {
	Default ResourceLimits `yaml:"default"`
	Max     ResourceLimits `yaml:"max"`
}
type ResourceLimits struct {
	CPUMillicores int `yaml:"cpu_millicores"`
	MemoryMB      int `yaml:"memory_mb"`
	PIDs          int `yaml:"pids"`
	DiskMB        int `yaml:"disk_mb"`
	IdleTTLS      int `yaml:"idle_ttl_s"`
	TTLS          int `yaml:"ttl_s"`
}
type Capacity struct {
	MemoryOvercommit float64 `yaml:"memory_overcommit"`
	CPUOvercommit    float64 `yaml:"cpu_overcommit"`
	QueueWaitS       int     `yaml:"queue_wait_s"`
	MinFreeDiskMB    int     `yaml:"min_free_disk_mb"`
}
type Pool struct {
	Enabled        bool     `yaml:"enabled"`
	PerImage       int      `yaml:"per_image"`
	MemoryBudgetMB int      `yaml:"memory_budget_mb"`
	Images         []string `yaml:"images"`
}
type Egress struct {
	AllowInternetMode bool     `yaml:"allow_internet_mode"`
	ExtraBlockedCIDRs []string `yaml:"extra_blocked_cidrs"`
}
type Retention struct {
	OutputDays int `yaml:"output_days"`
	AuditDays  int `yaml:"audit_days"`
}
type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}
type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// Defaults returns conservative service defaults.
func Defaults() Config {
	return Config{Listen: "127.0.0.1:8080", DataDir: "~/.local/share/cellbox", Backend: "native", DockerSocket: "/var/run/docker.sock", Limits: Limits{Default: ResourceLimits{1000, 512, 256, 1024, 600, 3600}, Max: ResourceLimits{4000, 4096, 2048, 10240, 0, 86400}}, Capacity: Capacity{MemoryOvercommit: 1, CPUOvercommit: 4, QueueWaitS: 5, MinFreeDiskMB: 1024}, Pool: Pool{Enabled: true, PerImage: 2, MemoryBudgetMB: 1024, Images: []string{"python:3.12-slim", "node:22-slim", "golang:1.23", "debian:bookworm-slim"}}, Retention: Retention{7, 30}, Log: Log{"info", "json"}}
}

// Load reads YAML, applies CELLBOX_* overrides, expands the data directory and validates values.
func Load(path string) (Config, error) {
	c := Defaults()
	if path == "" {
		if v, ok := os.LookupEnv("CELLBOX_CONFIG"); ok {
			path = v
		} else if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".config", "cellbox", "cellbox.yaml")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
			} else if _, err := os.Stat("/etc/cellbox/cellbox.yaml"); err == nil {
				path = "/etc/cellbox/cellbox.yaml"
			}
		}
	}
	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return c, fmt.Errorf("read config: %w", e)
		}
		d := yaml.NewDecoder(strings.NewReader(string(b)))
		d.KnownFields(true)
		if e = d.Decode(&c); e != nil {
			return c, fmt.Errorf("parse config: %w", e)
		}
	}
	for key, dst := range map[string]*string{"CELLBOX_LISTEN": &c.Listen, "CELLBOX_DATA_DIR": &c.DataDir, "CELLBOX_BACKEND": &c.Backend, "CELLBOX_DOCKER_SOCKET": &c.DockerSocket, "CELLBOX_LOG_LEVEL": &c.Log.Level, "CELLBOX_LOG_FORMAT": &c.Log.Format, "CELLBOX_TLS_CERT": &c.TLS.Cert, "CELLBOX_TLS_KEY": &c.TLS.Key} {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	if strings.HasPrefix(c.DataDir, "~/") {
		h, e := os.UserHomeDir()
		if e != nil {
			return c, fmt.Errorf("resolve home directory: %w", e)
		}
		c.DataDir = filepath.Join(h, c.DataDir[2:])
	}
	if e := c.Validate(); e != nil {
		return c, e
	}
	return c, nil
}

// Validate rejects unsupported or unsafe configuration values.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen must not be empty")
	}
	_, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("listen must be host:port: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("listen port must be between 1 and 65535, got %q", port)
	}
	if c.DataDir == "" {
		return errors.New("data_dir must not be empty")
	}
	if c.Backend != "native" && c.Backend != "docker" {
		return fmt.Errorf("backend must be native or docker, got %q", c.Backend)
	}
	if c.Backend == "docker" && strings.TrimSpace(c.DockerSocket) == "" {
		return errors.New("docker_socket must not be empty for the docker backend")
	}
	if c.Log.Level != "debug" && c.Log.Level != "info" && c.Log.Level != "warn" && c.Log.Level != "error" {
		return fmt.Errorf("invalid log.level %q", c.Log.Level)
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		return fmt.Errorf("log.format must be json or text, got %q", c.Log.Format)
	}
	if (c.TLS.Cert == "") != (c.TLS.Key == "") {
		return errors.New("tls.cert and tls.key must be configured together")
	}
	if c.Limits.Default.CPUMillicores <= 0 || c.Limits.Default.MemoryMB <= 0 || c.Limits.Default.PIDs <= 0 || c.Limits.Default.DiskMB <= 0 || c.Limits.Default.TTLS <= 0 || c.Limits.Default.IdleTTLS <= 0 {
		return errors.New("default resource limits must be positive")
	}
	if c.Limits.Max.CPUMillicores < c.Limits.Default.CPUMillicores || c.Limits.Max.MemoryMB < c.Limits.Default.MemoryMB || c.Limits.Max.PIDs < c.Limits.Default.PIDs || c.Limits.Max.DiskMB < c.Limits.Default.DiskMB || c.Limits.Max.TTLS < c.Limits.Default.TTLS {
		return errors.New("maximum resource limits must be at least the defaults")
	}
	if c.Limits.Max.TTLS > 86400 {
		return errors.New("limits.max.ttl_s cannot exceed 86400")
	}
	if c.Capacity.MemoryOvercommit <= 0 || c.Capacity.CPUOvercommit <= 0 || c.Capacity.QueueWaitS < 0 || c.Capacity.MinFreeDiskMB <= 0 || int64(c.Capacity.MinFreeDiskMB) > (1<<63-1)/(1024*1024) {
		return errors.New("capacity ratios and min_free_disk_mb must be positive and queue_wait_s cannot be negative")
	}
	if c.Pool.PerImage < 0 || c.Pool.MemoryBudgetMB < 0 || c.Retention.OutputDays < 0 || c.Retention.AuditDays < 0 {
		return errors.New("pool and retention limits cannot be negative")
	}
	seenPoolImages := map[string]struct{}{}
	for _, image := range c.Pool.Images {
		image = strings.TrimSpace(image)
		if image == "" || len(image) > 512 || strings.ContainsAny(image, " \t\r\n") {
			return errors.New("pool image references must contain 1 to 512 characters without whitespace")
		}
		if _, exists := seenPoolImages[image]; exists {
			return fmt.Errorf("duplicate pool image reference %q", image)
		}
		seenPoolImages[image] = struct{}{}
	}
	for _, cidr := range c.Egress.ExtraBlockedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid egress.extra_blocked_cidrs entry %q: %w", cidr, err)
		}
	}
	return nil
}
