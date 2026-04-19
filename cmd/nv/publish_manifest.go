package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Perdonus/NV/internal/api"
	"github.com/Perdonus/NV/internal/semver"
)

type packageManifest struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Homepage    string                   `json:"homepage"`
	Aliases     []string                 `json:"aliases"`
	DistTags    []string                 `json:"dist_tags"`
	Readme      string                   `json:"readme"`
	Notes       string                   `json:"notes"`
	Variants    []packageManifestVariant `json:"variants"`
}

type packageManifestVariant struct {
	ID                string         `json:"id"`
	Label             string         `json:"label"`
	OS                string         `json:"os"`
	Default           bool           `json:"default"`
	IsDefault         bool           `json:"is_default"`
	Artifact          string         `json:"artifact"`
	FileName          string         `json:"file_name"`
	InstallStrategy   string         `json:"install_strategy"`
	UninstallStrategy string         `json:"uninstall_strategy"`
	InstallRoot       string         `json:"install_root"`
	BinaryName        string         `json:"binary_name"`
	WrapperName       string         `json:"wrapper_name"`
	LauncherPath      string         `json:"launcher_path"`
	Metadata          map[string]any `json:"metadata"`
}

type loadedPackageManifest struct {
	Path         string
	Dir          string
	Manifest     packageManifest
	ReadmePath   string
	NotesPath    string
	ArtifactPath map[string]string
}

func discoverManifestPath(candidate string) (string, error) {
	if trimmed := strings.TrimSpace(candidate); trimmed != "" {
		path, err := filepath.Abs(trimmed)
		if err != nil {
			return "", err
		}
		return path, nil
	}

	candidates := []string{
		"nv.package.json",
		"nv.json",
	}
	for _, candidate := range candidates {
		path, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", errors.New("не найден manifest пакета: используй nv.package.json, nv.json или --manifest <file>")
}

func loadPackageManifest(candidate string) (*loadedPackageManifest, error) {
	manifestPath, err := discoverManifestPath(candidate)
	if err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var manifest packageManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return nil, fmt.Errorf("manifest пакета повреждён: %w", err)
	}

	loaded := &loadedPackageManifest{
		Path:         manifestPath,
		Dir:          filepath.Dir(manifestPath),
		Manifest:     manifest,
		ArtifactPath: map[string]string{},
	}
	if err := loaded.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return loaded, nil
}

func (loaded *loadedPackageManifest) normalizeAndValidate() error {
	loaded.Manifest.Name = normalizePackageName(loaded.Manifest.Name)
	if strings.TrimSpace(loaded.Manifest.Name) == "" {
		return errors.New("manifest не содержит name")
	}
	if normalizedVersion, err := semver.Normalize(strings.TrimSpace(loaded.Manifest.Version)); err != nil {
		return fmt.Errorf("manifest содержит некорректную version: %w", err)
	} else {
		loaded.Manifest.Version = normalizedVersion
	}
	loaded.Manifest.Title = strings.TrimSpace(loaded.Manifest.Title)
	if loaded.Manifest.Title == "" {
		loaded.Manifest.Title = strings.TrimPrefix(loaded.Manifest.Name, "@")
	}
	loaded.Manifest.Description = strings.TrimSpace(loaded.Manifest.Description)
	loaded.Manifest.Homepage = strings.TrimSpace(loaded.Manifest.Homepage)

	seenAliases := map[string]struct{}{}
	aliases := make([]string, 0, len(loaded.Manifest.Aliases))
	for _, alias := range loaded.Manifest.Aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			continue
		}
		if _, exists := seenAliases[alias]; exists {
			continue
		}
		seenAliases[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	loaded.Manifest.Aliases = aliases

	distTags := make([]string, 0, len(loaded.Manifest.DistTags))
	seenTags := map[string]struct{}{}
	for _, tag := range loaded.Manifest.DistTags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seenTags[tag]; exists {
			continue
		}
		seenTags[tag] = struct{}{}
		distTags = append(distTags, tag)
	}
	if len(distTags) == 0 {
		distTags = []string{"latest"}
	}
	sort.Strings(distTags)
	loaded.Manifest.DistTags = distTags

	if readme := strings.TrimSpace(loaded.Manifest.Readme); readme != "" {
		loaded.ReadmePath = filepath.Join(loaded.Dir, filepath.FromSlash(readme))
		if _, err := os.Stat(loaded.ReadmePath); err != nil {
			return fmt.Errorf("readme не найден: %s", readme)
		}
	}
	if notes := strings.TrimSpace(loaded.Manifest.Notes); notes != "" {
		loaded.NotesPath = filepath.Join(loaded.Dir, filepath.FromSlash(notes))
		if _, err := os.Stat(loaded.NotesPath); err != nil {
			return fmt.Errorf("notes не найден: %s", notes)
		}
	}

	if len(loaded.Manifest.Variants) == 0 {
		return errors.New("manifest не содержит variants")
	}
	seenVariants := map[string]struct{}{}
	for index := range loaded.Manifest.Variants {
		variant := &loaded.Manifest.Variants[index]
		variant.ID = strings.TrimSpace(variant.ID)
		if variant.ID == "" {
			return errors.New("variant без id")
		}
		if _, exists := seenVariants[variant.ID]; exists {
			return fmt.Errorf("variant %s повторяется", variant.ID)
		}
		seenVariants[variant.ID] = struct{}{}
		variant.Label = strings.TrimSpace(variant.Label)
		variant.OS = strings.ToLower(strings.TrimSpace(variant.OS))
		if variant.OS == "" {
			return fmt.Errorf("variant %s не содержит os", variant.ID)
		}
		variant.Artifact = strings.TrimSpace(variant.Artifact)
		if variant.Artifact == "" {
			return fmt.Errorf("variant %s не содержит artifact", variant.ID)
		}
		artifactPath := filepath.Join(loaded.Dir, filepath.FromSlash(variant.Artifact))
		if info, err := os.Stat(artifactPath); err != nil {
			return fmt.Errorf("artifact для variant %s не найден: %s", variant.ID, variant.Artifact)
		} else if info.IsDir() {
			return fmt.Errorf("artifact для variant %s должен быть файлом: %s", variant.ID, variant.Artifact)
		}
		loaded.ArtifactPath[variant.ID] = artifactPath
		variant.FileName = strings.TrimSpace(variant.FileName)
		if variant.FileName == "" {
			variant.FileName = filepath.Base(artifactPath)
		}
		variant.InstallStrategy = strings.TrimSpace(variant.InstallStrategy)
		if variant.InstallStrategy == "" {
			return fmt.Errorf("variant %s не содержит install_strategy", variant.ID)
		}
		variant.UninstallStrategy = strings.TrimSpace(variant.UninstallStrategy)
		variant.InstallRoot = strings.TrimSpace(variant.InstallRoot)
		variant.BinaryName = strings.TrimSpace(variant.BinaryName)
		variant.WrapperName = strings.TrimSpace(variant.WrapperName)
		variant.LauncherPath = strings.TrimSpace(variant.LauncherPath)
		if variant.Metadata == nil {
			variant.Metadata = map[string]any{}
		}
	}
	return nil
}

func (loaded *loadedPackageManifest) publishRequest(dryRun bool) (api.PublishRequest, []byte, error) {
	payload, err := json.MarshalIndent(loaded.Manifest, "", "  ")
	if err != nil {
		return api.PublishRequest{}, nil, err
	}
	return api.PublishRequest{
		Manifest:      payload,
		ReadmePath:    loaded.ReadmePath,
		NotesPath:     loaded.NotesPath,
		ArtifactPaths: loaded.ArtifactPath,
		DryRun:        dryRun,
	}, payload, nil
}
