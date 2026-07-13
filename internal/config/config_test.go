package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing fixture config: %v", err)
	}
	return path
}

const validConfig = `
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50

signal:
  base_url: "http://signal-host:8080"
  username: "user"
  password: "pass"
  sender_number: "+15551234567"
  recipients:
    - "group.abc=="

sites:
  - name: "lychee"
    type: local
    url: "http://localhost:8081/health"
    restart_command: "docker compose restart lychee"
    working_dir: "/opt/lychee"
  - name: "example-remote"
    type: remote
    url: "https://example.com/"
`

func TestLoad_Valid(t *testing.T) {
	path := writeConfig(t, validConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.CheckInterval != 60*time.Second {
		t.Errorf("CheckInterval = %v, want 60s", cfg.CheckInterval)
	}
	if cfg.RestartRecheckDelay != 15*time.Second {
		t.Errorf("RestartRecheckDelay = %v, want 15s", cfg.RestartRecheckDelay)
	}
	if cfg.HistorySize != 50 {
		t.Errorf("HistorySize = %d, want 50", cfg.HistorySize)
	}
	if len(cfg.Sites) != 2 {
		t.Fatalf("len(Sites) = %d, want 2", len(cfg.Sites))
	}
	if cfg.Sites[0].Type != SiteLocal {
		t.Errorf("Sites[0].Type = %q, want local", cfg.Sites[0].Type)
	}
	if cfg.Sites[1].Type != SiteRemote {
		t.Errorf("Sites[1].Type = %q, want remote", cfg.Sites[1].Type)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "missing restart_command for local site",
			yaml: `
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50
signal:
  base_url: "http://signal-host:8080"
  sender_number: "+1"
  recipients: ["group.abc=="]
sites:
  - name: "lychee"
    type: local
    url: "http://localhost:8081/health"
    working_dir: "/opt/lychee"
`,
			wantErr: "restart_command is required",
		},
		{
			name: "restart_command set on remote site",
			yaml: `
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50
signal:
  base_url: "http://signal-host:8080"
  sender_number: "+1"
  recipients: ["group.abc=="]
sites:
  - name: "example"
    type: remote
    url: "https://example.com"
    restart_command: "echo hi"
`,
			wantErr: "must not be set for remote",
		},
		{
			name: "duplicate site names",
			yaml: `
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50
signal:
  base_url: "http://signal-host:8080"
  sender_number: "+1"
  recipients: ["group.abc=="]
sites:
  - name: "dup"
    type: remote
    url: "https://example.com"
  - name: "dup"
    type: remote
    url: "https://example.org"
`,
			wantErr: "duplicate site name",
		},
		{
			name: "bad duration string",
			yaml: `
check_interval: "not-a-duration"
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50
signal:
  base_url: "http://signal-host:8080"
  sender_number: "+1"
  recipients: ["group.abc=="]
sites:
  - name: "example"
    type: remote
    url: "https://example.com"
`,
			wantErr: "parsing",
		},
		{
			name: "empty sites",
			yaml: `
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50
signal:
  base_url: "http://signal-host:8080"
  sender_number: "+1"
  recipients: ["group.abc=="]
sites: []
`,
			wantErr: "sites must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
