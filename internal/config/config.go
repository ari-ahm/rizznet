package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database    DatabaseConfig    `yaml:"database"`
	SystemProxy SystemProxyConfig `yaml:"system_proxy"`
	Tester      TesterConfig      `yaml:"tester"`
	Collectors  []CollectorConfig `yaml:"collectors"`
	Categories  []CategoryConfig  `yaml:"categories"`
	Publishers  []PublisherConfig `yaml:"publishers"`
}

type DatabaseConfig struct {
	Path       string `yaml:"path"`
	MaxProxies int    `yaml:"max_proxies"`
}

type SystemProxyConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Category string `yaml:"category"` // Configurable category name
	Fallback string `yaml:"fallback"`
}

type TesterConfig struct {
	HealthTimeout time.Duration `yaml:"health_timeout"`
	SpeedTimeout  time.Duration `yaml:"speed_timeout"`

	EchoURL string `yaml:"echo_url"`

	GeoIPASNPath     string `yaml:"geoip_asn_path"`
	GeoIPCountryPath string `yaml:"geoip_country_path"`

	DirtyCheckURL string `yaml:"dirty_check_url"`
	SpeedTestURL  string `yaml:"speed_test_url"`

	WorkerCount    int `yaml:"worker_count"`
	AnnealBudgetMB int `yaml:"anneal_budget_mb"`
}

type CollectorConfig struct {
	Name   string                 `yaml:"name"`
	Type   string                 `yaml:"type"`
	Params map[string]interface{} `yaml:"params"`
}

type CategoryConfig struct {
	Name       string                 `yaml:"name"`
	Strategy   string                 `yaml:"strategy"`
	Weight     int                    `yaml:"weight"`
	BucketSize int                    `yaml:"bucket_size"`
	Params     map[string]interface{} `yaml:"params"`
}

type PublisherConfig struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`
	Categories []string               `yaml:"categories"`
	Params     map[string]interface{} `yaml:"params"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = "config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	// Defaults
	cfg.Tester.HealthTimeout = 8 * time.Second
	cfg.Tester.SpeedTimeout = 45 * time.Second
	cfg.Tester.EchoURL = "http://api.ipify.org"
	cfg.Tester.GeoIPASNPath = "GeoLite2-ASN.mmdb"
	cfg.Tester.GeoIPCountryPath = "GeoLite2-Country.mmdb"
	cfg.Tester.DirtyCheckURL = "https://developers.google.com"
	cfg.Tester.SpeedTestURL = "https://speed.cloudflare.com/__down?bytes=5000000"
	cfg.Tester.WorkerCount = 50
	cfg.Tester.AnnealBudgetMB = 500

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config yaml: %w", err)
	}

	// Validate Categories
	for i := range cfg.Categories {
		if cfg.Categories[i].BucketSize <= 0 {
			cfg.Categories[i].BucketSize = 20
		}
		if cfg.Categories[i].Weight <= 0 {
			cfg.Categories[i].Weight = 1
		}
	}

	return &cfg, nil
}

func (c *Config) FilterCollectors(names []string) {
	if len(names) == 0 {
		return
	}
	whitelist := make(map[string]bool)
	for _, n := range names {
		whitelist[n] = true
	}
	var filtered []CollectorConfig
	for _, item := range c.Collectors {
		if whitelist[item.Name] {
			filtered = append(filtered, item)
		}
	}
	c.Collectors = filtered
}

func (c *Config) FilterPublishers(names []string) {
	if len(names) == 0 {
		return
	}
	whitelist := make(map[string]bool)
	for _, n := range names {
		whitelist[n] = true
	}
	var filtered []PublisherConfig
	for _, item := range c.Publishers {
		if whitelist[item.Name] {
			filtered = append(filtered, item)
		}
	}
	c.Publishers = filtered
}

func (c *Config) FilterCategories(names []string) {
	if len(names) == 0 {
		return
	}
	whitelist := make(map[string]bool)
	for _, n := range names {
		whitelist[n] = true
	}
	var filtered []CategoryConfig
	for _, item := range c.Categories {
		if whitelist[item.Name] {
			filtered = append(filtered, item)
		}
	}
	c.Categories = filtered
}
