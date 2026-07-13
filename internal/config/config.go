// Package config loads and validates the keep-me-alive YAML configuration via Viper.
package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/viper"
)

// SiteType identifies whether a monitored site is restarted locally or only checked.
type SiteType string

const (
	SiteLocal  SiteType = "local"
	SiteRemote SiteType = "remote"
)

// SignalConfig holds connection details for the signal-cli-rest-api service.
type SignalConfig struct {
	BaseURL      string   `mapstructure:"base_url"`
	Username     string   `mapstructure:"username"`
	Password     string   `mapstructure:"password"`
	SenderNumber string   `mapstructure:"sender_number"`
	Recipients   []string `mapstructure:"recipients"`
}

// SiteConfig describes a single monitored website.
type SiteConfig struct {
	Name           string   `mapstructure:"name"`
	Type           SiteType `mapstructure:"type"`
	URL            string   `mapstructure:"url"`
	RestartCommand string   `mapstructure:"restart_command"`
	WorkingDir     string   `mapstructure:"working_dir"`
}

// Config is the fully parsed and validated application configuration.
type Config struct {
	CheckInterval       time.Duration `mapstructure:"check_interval"`
	RestartRecheckDelay time.Duration `mapstructure:"restart_recheck_delay"`
	HTTPTimeout         time.Duration `mapstructure:"http_timeout"`
	StateDBPath         string        `mapstructure:"state_db_path"`
	HistorySize         int           `mapstructure:"history_size"`
	Signal              SignalConfig  `mapstructure:"signal"`
	Sites               []SiteConfig  `mapstructure:"sites"`
}

// Load reads the YAML file at path via Viper, unmarshals it into a Config
// (duration fields are decoded natively from strings like "60s"), and
// validates the result.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive")
	}
	if c.RestartRecheckDelay <= 0 {
		return fmt.Errorf("restart_recheck_delay must be positive")
	}
	if c.HTTPTimeout <= 0 {
		return fmt.Errorf("http_timeout must be positive")
	}
	if c.StateDBPath == "" {
		return fmt.Errorf("state_db_path must not be empty")
	}
	if c.HistorySize <= 0 {
		return fmt.Errorf("history_size must be positive")
	}

	if c.Signal.BaseURL == "" {
		return fmt.Errorf("signal.base_url must not be empty")
	}
	if c.Signal.SenderNumber == "" {
		return fmt.Errorf("signal.sender_number must not be empty")
	}
	if len(c.Signal.Recipients) == 0 {
		return fmt.Errorf("signal.recipients must not be empty")
	}

	if len(c.Sites) == 0 {
		return fmt.Errorf("sites must not be empty")
	}

	seen := make(map[string]bool, len(c.Sites))
	for i, s := range c.Sites {
		if s.Name == "" {
			return fmt.Errorf("sites[%d]: name must not be empty", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("sites[%d]: duplicate site name %q", i, s.Name)
		}
		seen[s.Name] = true

		if _, err := url.ParseRequestURI(s.URL); err != nil {
			return fmt.Errorf("sites[%d] (%s): invalid url %q: %w", i, s.Name, s.URL, err)
		}

		switch s.Type {
		case SiteLocal:
			if s.RestartCommand == "" {
				return fmt.Errorf("sites[%d] (%s): restart_command is required for local sites", i, s.Name)
			}
			if s.WorkingDir == "" {
				return fmt.Errorf("sites[%d] (%s): working_dir is required for local sites", i, s.Name)
			}
		case SiteRemote:
			if s.RestartCommand != "" {
				return fmt.Errorf("sites[%d] (%s): restart_command must not be set for remote sites", i, s.Name)
			}
			if s.WorkingDir != "" {
				return fmt.Errorf("sites[%d] (%s): working_dir must not be set for remote sites", i, s.Name)
			}
		default:
			return fmt.Errorf("sites[%d] (%s): type must be %q or %q, got %q", i, s.Name, SiteLocal, SiteRemote, s.Type)
		}
	}

	return nil
}
