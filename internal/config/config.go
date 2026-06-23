package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	MySQL       MySQLConfig       `json:"mysql"`
	Tonghuashun TonghuashunConfig `json:"tonghuashun"`
	Server      ServerConfig      `json:"server"`
}

type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Charset  string `json:"charset"`
}

type TonghuashunConfig struct {
	SectorURL         string `json:"sector_url"`
	StockURLTemplate  string `json:"stock_url_template"`
	UserAgent         string `json:"user_agent"`
	VJSPath           string `json:"v_js_path"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	StockMaxPages     int    `json:"stock_max_pages"`
	RequestSleepMinMS int    `json:"request_sleep_min_ms"`
	RequestSleepMaxMS int    `json:"request_sleep_max_ms"`
	RetryCount        int    `json:"retry_count"`
	RetrySleepMS      int    `json:"retry_sleep_ms"`
}

type ServerConfig struct {
	Addr string `json:"addr"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.MySQL.Host == "" {
		return fmt.Errorf("mysql.host is required")
	}
	if c.MySQL.Port == 0 {
		return fmt.Errorf("mysql.port is required")
	}
	if c.MySQL.Username == "" {
		return fmt.Errorf("mysql.username is required")
	}
	if c.MySQL.Database == "" {
		return fmt.Errorf("mysql.database is required")
	}
	if c.MySQL.Charset == "" {
		c.MySQL.Charset = "utf8mb4"
	}
	if c.Tonghuashun.SectorURL == "" {
		return fmt.Errorf("tonghuashun.sector_url is required")
	}
	if c.Tonghuashun.StockURLTemplate == "" {
		return fmt.Errorf("tonghuashun.stock_url_template is required")
	}
	if c.Tonghuashun.UserAgent == "" {
		c.Tonghuashun.UserAgent = "Mozilla/5.0"
	}
	if c.Tonghuashun.VJSPath == "" {
		c.Tonghuashun.VJSPath = "/etc/v.js"
	}
	if c.Tonghuashun.TimeoutSeconds == 0 {
		c.Tonghuashun.TimeoutSeconds = 20
	}
	if c.Tonghuashun.StockMaxPages == 0 {
		c.Tonghuashun.StockMaxPages = 80
	}
	if c.Tonghuashun.RequestSleepMinMS == 0 {
		c.Tonghuashun.RequestSleepMinMS = 3000
	}
	if c.Tonghuashun.RequestSleepMaxMS == 0 {
		c.Tonghuashun.RequestSleepMaxMS = 7000
	}
	if c.Tonghuashun.RequestSleepMaxMS < c.Tonghuashun.RequestSleepMinMS {
		return fmt.Errorf("tonghuashun.request_sleep_max_ms must be greater than or equal to request_sleep_min_ms")
	}
	if c.Tonghuashun.RetryCount == 0 {
		c.Tonghuashun.RetryCount = 3
	}
	if c.Tonghuashun.RetrySleepMS == 0 {
		c.Tonghuashun.RetrySleepMS = 10000
	}
	if c.Server.Addr == "" {
		c.Server.Addr = "0.0.0.0:8080"
	}
	return nil
}
