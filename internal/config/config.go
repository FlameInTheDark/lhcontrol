package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	RenamedStations map[string]string `json:"renamedStations"`
}

// NewConfig creates a new Config with defaults
func NewConfig() *Config {
	return &Config{
		RenamedStations: make(map[string]string),
	}
}

// Helper function to get the full path to the config file
func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config dir: %w", err)
	}
	appConfigDir := filepath.Join(configDir, "lhcontrol")
	err = os.MkdirAll(appConfigDir, 0755) // Ensure the directory exists
	if err != nil {
		return "", fmt.Errorf("failed to create app config dir '%s': %w", appConfigDir, err)
	}
	return filepath.Join(appConfigDir, "config.json"), nil
}

// Load reads the configuration from disk
func (c *Config) Load() error {
	configFilePath, err := getConfigPath()
	if err != nil {
		return err
	}

	log.Printf("Loading config from: %s", configFilePath)
	configFile, err := os.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No config file yet, which is fine
		}
		return fmt.Errorf("error reading config file '%s': %w", configFilePath, err)
	}

	err = json.Unmarshal(configFile, c)
	if err != nil {
		return fmt.Errorf("error unmarshalling config: %w", err)
	}
	// Ensure map is initialized if unmarshal left it nil
	if c.RenamedStations == nil {
		c.RenamedStations = make(map[string]string)
	}
	return nil
}

// Save writes the configuration to disk
func (c *Config) Save() error {
	configFilePath, err := getConfigPath()
	if err != nil {
		return err
	}

	configFile, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}

	log.Printf("Saving config to: %s", configFilePath)
	err = os.WriteFile(configFilePath, configFile, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file '%s': %w", configFilePath, err)
	}
	return nil
}
