// Package config loads application configuration without exposing secret values.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DatabaseConnectionURL string
	FredAPIKey            string
}

func Load(dotenvPath string) (Config, error) {
	if err := loadDotenv(dotenvPath); err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	config := Config{
		DatabaseConnectionURL: os.Getenv("DATABASE_CONNECTION_URL"),
		FredAPIKey:            os.Getenv("FRED_API_KEY"),
	}
	if config.FredAPIKey == "" {
		return Config{}, fmt.Errorf("FRED_API_KEY is required")
	}
	return config, nil
}

// loadDotenv is intentionally small: production secrets are supplied by the
// process environment, which always takes precedence over the local .env file.
func loadDotenv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid dotenv entry")
		}
		if _, exists := os.LookupEnv(strings.TrimSpace(key)); !exists {
			os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), "\"'"))
		}
	}
	return scanner.Err()
}
