package serverconfig

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

const (
	appName               = "sandbox-mcp"
	defaultConfigFileName = "serverConfig.yaml"
)

// Config holds the core configuration for sandbox-mcp
type Config struct {
	// SandboxesPath is the path to the sandboxes directory
	ServerPaths []string `yaml:"serverPaths"`
}

// LoadConfig loads the configuration from the serverConfig.json file
// If the config file doesn't exist, it creates one with default values
func LoadConfig() (*Config, error) {
	configPath := filepath.Join(xdg.ConfigHome, appName, defaultConfigFileName)
	log.Printf("Looking for config file at: %s", configPath)

	// Try to read existing config
	config := &Config{}
	if data, err := os.ReadFile(configPath); err == nil {
		log.Printf("Found existing config file, attempting to parse")
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
		log.Printf("Successfully loaded existing config with server paths: %s", config.ServerPaths)
		return config, nil
	} else {
		log.Printf("No existing config file found: %v", err)
	}

	return config, nil
}
