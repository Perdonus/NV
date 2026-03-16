package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Perdonus/NV/internal/api"
)

const schemaVersion = 1

type File struct {
	SchemaVersion int                         `json:"schema_version"`
	Packages      map[string]InstalledPackage `json:"packages"`
}

type InstalledPackage struct {
	Package     api.ResolvedPackage `json:"package"`
	InstalledAt string              `json:"installed_at"`
	UpdatedAt   string              `json:"updated_at"`
}

func New() *File {
	return &File{
		SchemaVersion: schemaVersion,
		Packages:      map[string]InstalledPackage{},
	}
}

func Load() (*File, error) {
	statePath, err := DefaultPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}

	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion == 0 {
		file.SchemaVersion = schemaVersion
	}
	if file.SchemaVersion != schemaVersion {
		return nil, errors.New("unsupported state schema version")
	}
	if file.Packages == nil {
		file.Packages = map[string]InstalledPackage{}
	}

	normalizedPackages := make(map[string]InstalledPackage, len(file.Packages))
	for name, record := range file.Packages {
		normalizedPackages[normalizeName(name)] = record
	}
	file.Packages = normalizedPackages
	return &file, nil
}

func Save(file *File) error {
	statePath, err := DefaultPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(statePath), ".nv-state-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(payload); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tempPath, statePath)
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "NV", "state.json"), nil
	}

	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "nv", "packages.json"), nil
	}
	return filepath.Join(home, ".local", "state", "nv", "packages.json"), nil
}

func (f *File) Get(name string) (InstalledPackage, bool) {
	record, ok := f.Packages[normalizeName(name)]
	return record, ok
}

func (f *File) Put(pkg api.ResolvedPackage) {
	key := normalizeName(pkg.Name)
	now := time.Now().UTC().Format(time.RFC3339)
	installedAt := now
	if existing, ok := f.Packages[key]; ok && existing.InstalledAt != "" {
		installedAt = existing.InstalledAt
	}
	f.Packages[key] = InstalledPackage{
		Package:     pkg,
		InstalledAt: installedAt,
		UpdatedAt:   now,
	}
}

func (f *File) Delete(name string) {
	delete(f.Packages, normalizeName(name))
}

func (f *File) Names() []string {
	names := make([]string, 0, len(f.Packages))
	for name := range f.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
