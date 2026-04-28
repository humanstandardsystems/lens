package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	ResetDay      string `toml:"reset_day"`
	ResetHour     int    `toml:"reset_hour"`
	ResetTimezone string `toml:"reset_timezone"`
	DBPath        string `toml:"db_path"`
}

func lensDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lens")
}

func configPath() string {
	return filepath.Join(lensDir(), "config.toml")
}

func loadConfig() (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(configPath(), &cfg); err != nil {
		return cfg, err
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(lensDir(), "lens.db")
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	if err := os.MkdirAll(lensDir(), 0755); err != nil {
		return err
	}
	f, err := os.Create(configPath())
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func weekStart(cfg Config) time.Time {
	loc, err := time.LoadLocation(cfg.ResetTimezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	dayMap := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	resetDay, ok := dayMap[strings.ToLower(cfg.ResetDay)]
	if !ok {
		resetDay = time.Tuesday
	}

	candidate := time.Date(now.Year(), now.Month(), now.Day(), cfg.ResetHour, 0, 0, 0, loc)
	for candidate.Weekday() != resetDay || candidate.After(now) {
		candidate = candidate.Add(-24 * time.Hour)
	}
	return candidate
}

func detectTimezone() (string, error) {
	dest, err := os.Readlink("/etc/localtime")
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(dest, "zoneinfo/", 2)
	if len(parts) == 2 {
		return parts[1], nil
	}
	return "UTC", nil
}
