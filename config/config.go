package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Port                 int      `yaml:"port"`
	BindAddress          string   `yaml:"bind_address"`
	AllowedOrigins       []string `yaml:"allowed_origins"`
	LogLevel             string   `yaml:"log_level"`
	LogFile              string   `yaml:"log_file"`
	CardPollIntervalMs   int      `yaml:"card_poll_interval_ms"`
	SignTimeoutSeconds   int      `yaml:"sign_timeout_seconds"`
	PKCS11Module         string   `yaml:"pkcs11_module"`
}

var defaultConfig = Config{
	Port:               19711,
	BindAddress:        "127.0.0.1",
	AllowedOrigins:     []string{"http://localhost:3000"},
	LogLevel:           "info",
	LogFile:            "",
	CardPollIntervalMs: 500,
	SignTimeoutSeconds: 30,
	PKCS11Module:       "",
}

// Load loads configuration from file or environment, with defaults
func Load(configPath string) (*Config, error) {
	cfg := defaultConfig

	// If no config path provided, search default locations
	if configPath == "" {
		configPath = findConfigFile()
	}

	// If config file exists, load it
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config: %w", err)
			}
		}
	}

	return &cfg, nil
}

func findConfigFile() string {
	searchPaths := []string{
		"./config.yaml",
		"./config.yml",
	}

	// Platform-specific paths
	switch {
	case isWindows():
		appData := os.Getenv("APPDATA")
		if appData != "" {
			searchPaths = append(searchPaths, filepath.Join(appData, "SrbIdMiddleware", "config.yaml"))
		}
	case isMacOS():
		home := os.Getenv("HOME")
		if home != "" {
			searchPaths = append(searchPaths, filepath.Join(home, "Library", "Application Support", "SrbIdMiddleware", "config.yaml"))
		}
	}

	// Linux and fallback
	searchPaths = append(searchPaths, "/etc/locksmith/config.yaml")

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func isWindows() bool {
	return os.Getenv("OS") == "Windows_NT"
}

func isMacOS() bool {
	return os.Getenv("GOOS") == "darwin"
}
