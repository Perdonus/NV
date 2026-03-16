package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/Perdonus/NV/internal/api"
	"github.com/Perdonus/NV/internal/state"
)

func listInstalledPackages() error {
	installedState, err := state.Load()
	if err != nil {
		return fmt.Errorf("не удалось открыть локальное состояние пакетов: %w", err)
	}

	names := installedState.Names()
	if len(names) == 0 {
		fmt.Println("Установленных пакетов нет.")
		return nil
	}

	for _, name := range names {
		record, ok := installedState.Get(name)
		if !ok {
			continue
		}
		line := fmt.Sprintf("%s %s", record.Package.Name, record.Package.ResolvedVersion)
		if label := strings.TrimSpace(record.Package.Variant.Label); label != "" {
			line += fmt.Sprintf(" [%s]", label)
		}
		fmt.Println(line)
	}
	return nil
}

func searchPackages(client *api.Client, query string) error {
	catalog, err := client.ListPackages(runtime.GOOS)
	if err != nil {
		return fmt.Errorf("реестр пакетов недоступен: %w", err)
	}
	if !catalog.Success && strings.TrimSpace(catalog.Error) != "" {
		return errors.New(strings.TrimSpace(catalog.Error))
	}

	installedState := safeInstalledState()
	query = strings.ToLower(strings.TrimSpace(query))
	matches := 0
	for _, pkg := range catalog.Packages {
		if !matchesPackageQuery(pkg, query) {
			continue
		}
		matches++
		latestVersion := normalizedCatalogVersion(pkg.Name, pkg.LatestVersion)
		line := fmt.Sprintf("%s %s", pkg.Name, latestVersion)
		if installed, ok := installedState.Get(pkg.Name); ok {
			line += fmt.Sprintf(" installed:%s", installed.Package.ResolvedVersion)
		}
		fmt.Println(line)
		if description := strings.TrimSpace(pkg.Description); description != "" {
			fmt.Printf("  %s\n", description)
		}
	}
	if matches == 0 {
		fmt.Println("Пакеты не найдены.")
	}
	return nil
}

func showPackageInfo(client *api.Client, name string) error {
	normalized := normalizePackageName(name)
	if normalized == "" {
		return errors.New("empty package name")
	}

	details, err := client.PackageDetails(normalized, runtime.GOOS)
	if err != nil {
		return fmt.Errorf("реестр пакетов недоступен: %w", err)
	}
	if !details.Success && strings.TrimSpace(details.Error) != "" {
		return errors.New(strings.TrimSpace(details.Error))
	}

	pkg := details.Package
	fmt.Printf("Name: %s\n", pkg.Name)
	if title := strings.TrimSpace(pkg.Title); title != "" {
		fmt.Printf("Title: %s\n", title)
	}
	if description := strings.TrimSpace(pkg.Description); description != "" {
		fmt.Printf("Description: %s\n", description)
	}
	if homepage := strings.TrimSpace(pkg.Homepage); homepage != "" {
		fmt.Printf("Homepage: %s\n", homepage)
	}
	if latestVersion := normalizedCatalogVersion(pkg.Name, pkg.LatestVersion); latestVersion != "" && latestVersion != "unknown" {
		fmt.Printf("Latest: %s\n", latestVersion)
	}
	if installed, ok := safeInstalledState().Get(pkg.Name); ok {
		fmt.Printf("Installed: %s\n", installed.Package.ResolvedVersion)
	}

	fmt.Println("Variants:")
	for _, variant := range pkg.Variants {
		line := fmt.Sprintf("  %s %s", variant.ID, normalizedCatalogVersion(pkg.Name, variant.Version))
		if label := strings.TrimSpace(variant.Label); label != "" {
			line += fmt.Sprintf(" %s", label)
		}
		if variant.OS != "" {
			line += fmt.Sprintf(" os=%s", variant.OS)
		}
		if variant.InstallStrategy != "" {
			line += fmt.Sprintf(" strategy=%s", variant.InstallStrategy)
		}
		if variant.IsDefault || variant.Default {
			line += " default"
		}
		fmt.Println(line)
	}
	return nil
}

func safeInstalledState() *state.File {
	installedState, err := state.Load()
	if err != nil {
		return state.New()
	}
	return installedState
}

func matchesPackageQuery(pkg api.PackageRecord, query string) bool {
	if query == "" {
		return true
	}
	fields := []string{pkg.Name, pkg.Title, pkg.Description}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func normalizedCatalogVersion(packageName, version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return "unknown"
	}
	normalized, err := normalizePackageVersion(packageName, trimmed)
	if err != nil {
		return trimmed
	}
	return normalized
}
