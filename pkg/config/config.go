package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultPath    = "/etc/zjunet-go/config.json"
	DefaultLNS     = "10.5.1.9"
	DefaultLACName = "zjunet-go"
	DefaultMTU     = 1428
	DefaultDNS     = "10.10.0.21"
)

type Config struct {
	User        string   `json:"user"`
	Password    string   `json:"password"`
	LNS         string   `json:"lns"`
	LACName     string   `json:"lac_name"`
	MTU         int      `json:"mtu"`
	DNS         []string `json:"dns"`
	ManageDNS   bool     `json:"manage_dns"`
	ManageRoute bool     `json:"manage_route"`
	ManageNAT   bool     `json:"manage_nat"`
}

func Default() Config {
	return Config{
		LNS:         DefaultLNS,
		LACName:     DefaultLACName,
		MTU:         DefaultMTU,
		DNS:         []string{DefaultDNS},
		ManageRoute: true,
	}
}

func (c Config) Validate() error {
	if c.User == "" {
		return errors.New("missing user in config file")
	}
	if c.Password == "" {
		return errors.New("missing password in config file")
	}
	if strings.ContainsAny(c.User+c.Password+c.LNS+c.LACName, "\r\n") {
		return errors.New("config values must not contain newlines")
	}
	if c.LNS == "" || c.LACName == "" {
		return errors.New("lns and lac-name are required")
	}
	if c.MTU <= 0 {
		return errors.New("mtu must be positive")
	}
	return nil
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func FirstDNS(cfg Config) string {
	if len(cfg.DNS) == 0 || cfg.DNS[0] == "" {
		return DefaultDNS
	}
	return cfg.DNS[0]
}
