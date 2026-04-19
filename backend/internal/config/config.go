package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig    `toml:"server"`
	Frontend  FrontendConfig  `toml:"frontend"`
	Database  DatabaseConfig  `toml:"database"`
	OpenClaw  OpenClawConfig  `toml:"openclaw"`
	WebSocket WebSocketConfig `toml:"websocket"`
	Logging   LoggingConfig   `toml:"logging"`
	ClickUp   ClickUpConfig   `toml:"clickup"`
}

// ClickUpConfig configures the ClickUp v2 API client. Leave APIToken empty to
// disable; the /api/v1/clickup/* endpoints will then return a 503 with a
// "not configured" error instead of crashing.
type ClickUpConfig struct {
	APIToken    string `toml:"api_token"`
	WorkspaceID string `toml:"workspace_id"`
	ListID      string `toml:"list_id"`
}

type ServerConfig struct {
	Host         string `toml:"host"`
	Port         int    `toml:"port"`
	ReadTimeout  string `toml:"read_timeout"`
	WriteTimeout string `toml:"write_timeout"`
}

type FrontendConfig struct {
	StaticDir string `toml:"static_dir"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type OpenClawConfig struct {
	GatewayURL      string `toml:"gateway_url"`
	ReconnectDelay  string `toml:"reconnect_delay"`
	MaxReconnectDel string `toml:"max_reconnect_delay"`
	Token           string `toml:"token"`
}

type WebSocketConfig struct {
	PingInterval string `toml:"ping_interval"`
	PongTimeout  string `toml:"pong_timeout"`
}

type LoggingConfig struct {
	Level       string `toml:"level"`
	ServiceName string `toml:"service_name"`
}

func (s ServerConfig) ReadTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(s.ReadTimeout)
	if d == 0 {
		return 15 * time.Second
	}
	return d
}

func (s ServerConfig) WriteTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(s.WriteTimeout)
	if d == 0 {
		return 15 * time.Second
	}
	return d
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:   ServerConfig{Host: "127.0.0.1", Port: 8080, ReadTimeout: "15s", WriteTimeout: "15s"},
		Frontend: FrontendConfig{StaticDir: "../frontend"},
		Database: DatabaseConfig{Path: "data/command-centre.db"},
		OpenClaw: OpenClawConfig{GatewayURL: "ws://127.0.0.1:18789", ReconnectDelay: "2s", MaxReconnectDel: "30s"},
		WebSocket: WebSocketConfig{PingInterval: "30s", PongTimeout: "60s"},
		Logging:  LoggingConfig{Level: "info", ServiceName: "command-centre"},
	}

	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CC_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("CC_SERVER_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("CC_DATABASE_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("CC_OPENCLAW_GATEWAY_URL"); v != "" {
		cfg.OpenClaw.GatewayURL = v
	}
	if v := os.Getenv("CC_OPENCLAW_TOKEN"); v != "" {
		cfg.OpenClaw.Token = v
	}
	if v := os.Getenv("CC_FRONTEND_STATIC_DIR"); v != "" {
		cfg.Frontend.StaticDir = v
	}
	if v := os.Getenv("CC_CLICKUP_TOKEN"); v != "" {
		cfg.ClickUp.APIToken = v
	}
	if v := os.Getenv("CC_CLICKUP_WORKSPACE_ID"); v != "" {
		cfg.ClickUp.WorkspaceID = v
	}
	if v := os.Getenv("CC_CLICKUP_LIST_ID"); v != "" {
		cfg.ClickUp.ListID = v
	}
}
