package main

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/Perdonus/NV/internal/api"
	"github.com/Perdonus/NV/internal/semver"
	"github.com/Perdonus/NV/internal/state"
)

type outdatedEntry struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Variant   string `json:"variant,omitempty"`
	Installed string `json:"installed,omitempty"`
}

func outdatedCommand(client *api.Client, args []string) error {
	jsonOutput, positional, err := parseOutdatedArgs(args)
	if err != nil {
		return err
	}
	entries, err := collectOutdated(client, positional)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("Обновлений нет.")
		return nil
	}
	for _, entry := range entries {
		line := fmt.Sprintf("%s %s -> %s", entry.Name, entry.Current, entry.Latest)
		if entry.Variant != "" {
			line += fmt.Sprintf(" [%s]", entry.Variant)
		}
		fmt.Println(line)
	}
	return nil
}

func updateCommand(client *api.Client, args []string) error {
	targets := make([]string, 0, len(args))
	for _, argument := range args {
		argument = strings.TrimSpace(argument)
		if argument == "" {
			continue
		}
		if strings.HasPrefix(argument, "--") {
			return fmt.Errorf("usage: nv update [package ...]")
		}
		targets = append(targets, argument)
	}
	if len(targets) == 0 {
		entries, err := collectOutdated(client, nil)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("Обновлений нет.")
			return nil
		}
		targets = make([]string, 0, len(entries))
		for _, entry := range entries {
			targets = append(targets, entry.Name)
		}
	}

	seen := map[string]struct{}{}
	for _, target := range targets {
		name, _, err := parsePackageSpec(target)
		if err != nil {
			return err
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if err := installPackage(client, name+"@latest", installOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func parseOutdatedArgs(args []string) (bool, []string, error) {
	jsonOutput := false
	positional := make([]string, 0, len(args))
	for _, argument := range args {
		argument = strings.TrimSpace(argument)
		switch argument {
		case "":
			continue
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(argument, "--") {
				return false, nil, fmt.Errorf("usage: nv outdated [package ...] [--json]")
			}
			positional = append(positional, argument)
		}
	}
	return jsonOutput, positional, nil
}

func collectOutdated(client *api.Client, requested []string) ([]outdatedEntry, error) {
	installedState, err := state.Load()
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть локальное состояние пакетов: %w", err)
	}

	targets := make([]string, 0)
	if len(requested) == 0 {
		seen := map[string]struct{}{}
		for _, name := range installedState.Names() {
			canonical := canonicalPackageKey(name)
			if canonical == "" {
				continue
			}
			if _, exists := seen[canonical]; exists {
				continue
			}
			seen[canonical] = struct{}{}
			targets = append(targets, canonical)
		}
		sort.Strings(targets)
	} else {
		seen := map[string]struct{}{}
		for _, raw := range requested {
			name, _, err := parsePackageSpec(raw)
			if err != nil {
				return nil, err
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			targets = append(targets, name)
		}
	}

	entries := make([]outdatedEntry, 0, len(targets))
	for _, name := range targets {
		record, ok := getInstalledStateRecord(installedState, name)
		if !ok {
			return nil, fmt.Errorf("пакет %s не установлен", name)
		}
		latest, err := latestInstalledPackageVersion(client, name, record)
		if err != nil {
			return nil, err
		}
		current, err := normalizePackageVersion(name, record.Package.ResolvedVersion)
		if err != nil {
			current = strings.TrimSpace(record.Package.ResolvedVersion)
		}
		if semver.Compare(latest, current) <= 0 {
			continue
		}
		entry := outdatedEntry{
			Name:    canonicalPackageKey(name),
			Current: current,
			Latest:  latest,
			Variant: strings.TrimSpace(record.Package.Variant.Label),
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func latestInstalledPackageVersion(client *api.Client, packageName string, record state.InstalledPackage) (string, error) {
	if resolved, err := client.ResolvePackage(packageName, "latest", runtime.GOOS, strings.TrimSpace(record.Package.Variant.ID)); err == nil && resolved != nil {
		if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
			return "", errors.New(strings.TrimSpace(resolved.Error))
		}
		if latest, normalizeErr := normalizePackageVersion(packageName, resolved.Package.ResolvedVersion); normalizeErr == nil {
			return latest, nil
		}
	}
	return latestPackageVersion(client, packageName, runtime.GOOS)
}

func ensurePackageInstalled(installedState *state.File, name string) error {
	if _, ok := getInstalledStateRecord(installedState, name); ok {
		return nil
	}
	return errors.New("пакет не установлен")
}
