// Package config stores non-secret settings in ~/.config/mmt/config.json and
// the auth token in the OS keychain.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const keyringService = "mmt"

type Config struct {
	ServerURL   string `json:"server_url"`
	LastChannel string `json:"last_channel,omitempty"`
	// Terminal is the preferred terminal app: auto, iterm2, ghostty or current.
	Terminal string `json:"terminal,omitempty"`
	// Language of the interface: en or vi. Empty follows the locale.
	Language string `json:"language,omitempty"`
	// Browser opens links, e.g. "Google Chrome"; empty uses the system default.
	Browser string `json:"browser,omitempty"`
	// Lock is mmt's own app lock: "touchid", "passcode" or empty for none.
	Lock string `json:"lock,omitempty"`
	// LockAfter locks the app after this many idle minutes; 0 never.
	LockAfter int `json:"lock_after_minutes,omitempty"`
	// UnreadsOnly shows only conversations with unread messages in the sidebar.
	UnreadsOnly bool `json:"unreads_only,omitempty"`

	fromEnv bool // server came from MMT_URL; never persisted
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mmt", "config.json"), nil
}

// Load reads the config file. A missing file yields an empty config.
// MMT_URL overrides the stored server URL.
func Load() (*Config, error) {
	c := &Config{}
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, err
		}
	}
	if u := os.Getenv("MMT_URL"); u != "" {
		c.ServerURL = u
		c.fromEnv = true
	}
	c.ServerURL = NormalizeURL(c.ServerURL)
	return c, nil
}

// Update changes the saved config. It rereads the file first, so settings
// changed by other mmt commands while mmt was open are kept.
func Update(change func(*Config)) error {
	c, err := Load()
	if err != nil {
		return err
	}
	change(c)
	return c.Save()
}

func (c *Config) Save() error {
	if c.fromEnv {
		return nil
	}
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// NormalizeURL adds https:// when no scheme is given and trims trailing slashes.
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return strings.TrimRight(u, "/")
}

// Token returns the auth token for server. MMT_TOKEN overrides the keychain.
func Token(server string) (string, error) {
	if t := os.Getenv("MMT_TOKEN"); t != "" {
		return t, nil
	}
	return keyring.Get(keyringService, server)
}

func SetToken(server, token string) error {
	return keyring.Set(keyringService, server, token)
}

func DeleteToken(server string) error {
	err := keyring.Delete(keyringService, server)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
