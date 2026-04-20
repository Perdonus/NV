package main

import (
	"os"
	"strings"

	"github.com/Perdonus/NV/internal/config"
)

func loadNVConfig() (*config.File, error) {
	return config.Load()
}

func saveNVConfig(file *config.File) error {
	return config.Save(file)
}

func resolveAuthToken() string {
	if token := strings.TrimSpace(getenvFirst("NV_AUTH_TOKEN", "NEURALV_AUTH_TOKEN")); token != "" {
		return token
	}
	cfg, err := loadNVConfig()
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.AuthToken)
}

func resolvePublishToken() string {
	if token := strings.TrimSpace(getenvFirst("NV_PUBLISH_TOKEN", "NEURALV_PUBLISH_TOKEN")); token != "" {
		return token
	}
	return resolveAuthToken()
}

func resolvedBaseURL(defaultURL string) string {
	if override := strings.TrimSpace(getenvFirst("NV_BASE_URL", "NEURALV_BASE_URL")); override != "" {
		return strings.TrimRight(override, "/")
	}
	cfg, err := loadNVConfig()
	if err != nil || cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" {
		return strings.TrimRight(defaultURL, "/")
	}
	return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
}

func getenvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
