package configs

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DB         DBConfig         `yaml:"db"`
	Server     ServerConfig     `yaml:"server"`
	Blockchain BlockchainConfig `yaml:"blockchain"`
}

type DBConfig struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
}

type BlockchainConfig struct {
	AlchemyAPIURL string `yaml:"alchemy_url"`
	AlchemyAPIKey string `yaml:"alchemy_key"`
}

func LoadConfig(configPath string) (*Config, error) {
	// Set default config path if empty
	if configPath == "" {
		configPath = "config.yaml"
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found at %s", configPath)
	}

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	var missing []string

	if cfg.DB.Password == "" {
		missing = append(missing, "db.password")
	}
	if cfg.Blockchain.AlchemyAPIKey == "" {
		missing = append(missing, "blockchain.alchemy_key")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration fields: %v", missing)
	}

	return nil
}
