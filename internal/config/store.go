package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const schemaVersion = 1

type File struct {
	SchemaVersion int    `json:"schema_version"`
	BaseURL       string `json:"base_url,omitempty"`
	AuthToken     string `json:"auth_token,omitempty"`
}

func New() *File {
	return &File{
		SchemaVersion: schemaVersion,
	}
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		base := strings.TrimSpace(os.Getenv("APPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "NV", "config.json"), nil
	}

	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "nv", "config.json"), nil
	}
	return filepath.Join(home, ".config", "nv", "config.json"), nil
}

func Load() (*File, error) {
	configPath, err := DefaultPath()
	if err != nil {
		return nil, err
	}

	payload, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}

	var file File
	if err := json.Unmarshal(payload, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion == 0 {
		file.SchemaVersion = schemaVersion
	}
	if file.SchemaVersion != schemaVersion {
		return nil, errors.New("unsupported config schema version")
	}
	file.BaseURL = strings.TrimSpace(file.BaseURL)
	file.AuthToken = strings.TrimSpace(file.AuthToken)
	return &file, nil
}

func Save(file *File) error {
	configPath, err := DefaultPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	if file == nil {
		file = New()
	}
	if file.SchemaVersion == 0 {
		file.SchemaVersion = schemaVersion
	}
	file.BaseURL = strings.TrimSpace(file.BaseURL)
	file.AuthToken = strings.TrimSpace(file.AuthToken)

	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(configPath, payload, 0o600)
}

