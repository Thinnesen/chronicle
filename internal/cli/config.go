// Package cli is the chronicle command line tool.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config is the CLI configuration file. It holds credentials, so it is readable by the owner only.
type Config struct {
	Server    string `json:"server"`
	Namespace string `json:"namespace,omitempty"`
	// Token is a bearer token: an OIDC ID token or a service account token.
	Token  string    `json:"token,omitempty"`
	Expiry time.Time `json:"expiry,omitzero"`
	// RefreshToken, Issuer and ClientID renew the ID token of a device flow login.
	RefreshToken string   `json:"refreshToken,omitempty"`
	Issuer       string   `json:"issuer,omitempty"`
	ClientID     string   `json:"clientID,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// defaultConfigPath is $XDG_CONFIG_HOME/chronicle/config.json, usually ~/.config/chronicle/config.json.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "chronicle-config.json"
	}
	return filepath.Join(dir, "chronicle", "config.json")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("make the configuration directory: %w", err)
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
