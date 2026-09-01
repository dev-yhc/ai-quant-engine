// Package config loads application configuration without exposing secret values.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DatabaseConnectionURL    string
	FredAPIKey               string
	TemporalHostPort         string
	TemporalNamespace        string
	TemporalTaskQueue        string
	TemporalScheduleID       string
	TemporalScheduleCron     string
	TemporalScheduleTimeZone string
}

// DotenvPath resolves the optional local configuration file used by commands.
func DotenvPath() (string, error) {
	if configuredPath := os.Getenv("DATA_COLLECTOR_ENV_FILE"); configuredPath != "" {
		return configuredPath, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{
		filepath.Join(workingDirectory, ".env"),
		filepath.Join(workingDirectory, "apps", "data_collector", ".env"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return filepath.Join(workingDirectory, ".env"), nil
}

func Load(dotenvPath string) (Config, error) {
	if err := loadDotenv(dotenvPath); err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	config := fromEnvironment()
	if config.FredAPIKey == "" {
		return Config{}, fmt.Errorf("FRED_API_KEY is required")
	}
	if config.DatabaseConnectionURL == "" {
		return Config{}, fmt.Errorf("DATABASE_CONNECTION_URL is required")
	}
	return config, nil
}

// LoadSchedule loads only the settings necessary to register a schedule. It is
// intentionally independent of collector secrets because it can run in CI.
func LoadSchedule(dotenvPath string) (Config, error) {
	if err := loadDotenv(dotenvPath); err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	return fromEnvironment(), nil
}

func fromEnvironment() Config {
	return Config{
		DatabaseConnectionURL:    os.Getenv("DATABASE_CONNECTION_URL"),
		FredAPIKey:               os.Getenv("FRED_API_KEY"),
		TemporalHostPort:         valueOrDefault("TEMPORAL_HOST_PORT", "localhost:7233"),
		TemporalNamespace:        valueOrDefault("TEMPORAL_NAMESPACE", "default"),
		TemporalTaskQueue:        valueOrDefault("TEMPORAL_TASK_QUEUE", "data-collector"),
		TemporalScheduleID:       valueOrDefault("TEMPORAL_SCHEDULE_ID", "market-data-collection"),
		TemporalScheduleCron:     valueOrDefault("TEMPORAL_SCHEDULE_CRON", "0 6 * * 1-5"),
		TemporalScheduleTimeZone: valueOrDefault("TEMPORAL_SCHEDULE_TIME_ZONE", "Asia/Seoul"),
	}
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
