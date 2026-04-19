package backend

import (
	"encoding/json"
	"time"
)

const (
	LayoutVersion = "v1"
	SchemaVersion = 2
)

type Config struct {
	DataDir       string
	FilesDir      string
	SeedPath      string
	PublicBaseURL string
	PublishToken  string
}

type SeedCatalog struct {
	Packages []SeedPackage `json:"packages"`
}

type SeedPackage struct {
	Name        string        `json:"name"`
	Version     string        `json:"version,omitempty"`
	Aliases     []string      `json:"aliases"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Homepage    string        `json:"homepage"`
	DistTags    []string      `json:"dist_tags,omitempty"`
	Variants    []SeedVariant `json:"variants"`
}

type SeedVariant struct {
	ID                string         `json:"id"`
	Label             string         `json:"label"`
	OS                string         `json:"os"`
	IsDefault         bool           `json:"is_default"`
	Default           bool           `json:"default"`
	Version           string         `json:"version"`
	Channel           string         `json:"channel"`
	FileName          string         `json:"file_name"`
	DownloadURL       string         `json:"download_url"`
	InstallCommand    string         `json:"install_command"`
	SHA256            string         `json:"sha256"`
	InstallStrategy   string         `json:"install_strategy"`
	UninstallStrategy string         `json:"uninstall_strategy"`
	InstallRoot       string         `json:"install_root"`
	BinaryName        string         `json:"binary_name"`
	WrapperName       string         `json:"wrapper_name"`
	LauncherPath      string         `json:"launcher_path"`
	Notes             []string       `json:"notes"`
	Metadata          map[string]any `json:"metadata"`
	Source            map[string]any `json:"source"`
	UpdateCommand     string         `json:"update_command"`
	UpdatePolicy      string         `json:"update_policy"`
	AutoUpdate        bool           `json:"auto_update"`
}

type PackageRecord struct {
	Name          string           `json:"name"`
	Aliases       []string         `json:"aliases,omitempty"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Homepage      string           `json:"homepage"`
	LatestVersion string           `json:"latest_version"`
	Variants      []PackageVariant `json:"variants"`
}

type PackageVariant struct {
	ID                string         `json:"id"`
	Label             string         `json:"label"`
	OS                string         `json:"os"`
	IsDefault         bool           `json:"is_default"`
	Default           bool           `json:"default"`
	Version           string         `json:"version"`
	Channel           string         `json:"channel,omitempty"`
	FileName          string         `json:"file_name,omitempty"`
	DownloadURL       string         `json:"download_url,omitempty"`
	InstallCommand    string         `json:"install_command,omitempty"`
	SHA256            string         `json:"sha256,omitempty"`
	InstallStrategy   string         `json:"install_strategy,omitempty"`
	UninstallStrategy string         `json:"uninstall_strategy,omitempty"`
	InstallRoot       string         `json:"install_root,omitempty"`
	BinaryName        string         `json:"binary_name,omitempty"`
	WrapperName       string         `json:"wrapper_name,omitempty"`
	LauncherPath      string         `json:"launcher_path,omitempty"`
	Notes             []string       `json:"notes,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Source            map[string]any `json:"source,omitempty"`
	UpdateCommand     string         `json:"update_command,omitempty"`
	UpdatePolicy      string         `json:"update_policy,omitempty"`
	AutoUpdate        bool           `json:"auto_update,omitempty"`
}

type ResolvedPackage struct {
	Name            string         `json:"name"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Homepage        string         `json:"homepage"`
	LatestVersion   string         `json:"latest_version"`
	ResolvedVersion string         `json:"resolved_version"`
	Variant         PackageVariant `json:"variant"`
}

type PackageRelease struct {
	PackageName  string    `json:"package_name"`
	VariantID    string    `json:"variant_id"`
	Version      string    `json:"version"`
	FileName     string    `json:"file_name"`
	RelativePath string    `json:"relative_path"`
	DownloadURL  string    `json:"download_url"`
	ContentType  string    `json:"content_type,omitempty"`
	SHA256       string    `json:"sha256,omitempty"`
	Size         int64     `json:"size,omitempty"`
	PublishedAt  time.Time `json:"published_at"`
}

type PackageView struct {
	PackageRecord
	DistTags map[string]string  `json:"dist_tags"`
	Versions []string           `json:"versions"`
	Version  PackageVersionView `json:"version"`
	Releases []PackageRelease   `json:"releases,omitempty"`
}

type PackageVersionView struct {
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Homepage    string           `json:"homepage"`
	PublishedAt string           `json:"published_at,omitempty"`
	Readme      string           `json:"readme,omitempty"`
	Variants    []PackageVariant `json:"variants"`
	DistTags    []string         `json:"dist_tags,omitempty"`
}

type BootstrapManifest struct {
	LayoutVersion string             `json:"layout_version"`
	SchemaVersion int                `json:"schema_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	BaseURL       string             `json:"base_url,omitempty"`
	Packages      []BootstrapPackage `json:"packages"`
}

type BootstrapPackage struct {
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases,omitempty"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Homepage      string            `json:"homepage"`
	LatestVersion string            `json:"latest_version"`
	ViewURL       string            `json:"view_url,omitempty"`
	DetailsURL    string            `json:"details_url,omitempty"`
	ResolveURL    string            `json:"resolve_url,omitempty"`
	Variants      []BootstrapVariant `json:"variants"`
}

type BootstrapVariant struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	OS            string `json:"os,omitempty"`
	Default       bool   `json:"default,omitempty"`
	IsDefault     bool   `json:"is_default,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
	DownloadURL   string `json:"download_url,omitempty"`
}

type PublishRequest struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Variant        string         `json:"variant,omitempty"`
	FileName       string         `json:"file_name,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	ArtifactBase64 string        `json:"artifact_base64,omitempty"`
	Package        *SeedPackage   `json:"package,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type PublishResult struct {
	PackageRecord
	ResolvedVersion string `json:"resolved_version"`
	VariantID       string `json:"variant_id"`
	FileName        string `json:"file_name"`
	DownloadURL     string `json:"download_url"`
	RelativePath    string `json:"relative_path"`
	SHA256          string `json:"sha256,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Size            int64  `json:"size,omitempty"`
}

type sqlitePackageRow struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	LatestVersion string `json:"latest_version"`
	AliasesJSON   string `json:"aliases_json"`
}

type sqliteVariantRow struct {
	PackageName       string `json:"package_name"`
	VariantID         string `json:"variant_id"`
	Label             string `json:"label"`
	OS                string `json:"os"`
	IsDefault         int    `json:"is_default"`
	Default           int    `json:"default"`
	Version           string `json:"version"`
	Channel           string `json:"channel"`
	FileName          string `json:"file_name"`
	DownloadURL       string `json:"download_url"`
	InstallCommand    string `json:"install_command"`
	SHA256            string `json:"sha256"`
	InstallStrategy   string `json:"install_strategy"`
	UninstallStrategy string `json:"uninstall_strategy"`
	InstallRoot       string `json:"install_root"`
	BinaryName        string `json:"binary_name"`
	WrapperName       string `json:"wrapper_name"`
	LauncherPath      string `json:"launcher_path"`
	NotesJSON         string `json:"notes_json"`
	MetadataJSON      string `json:"metadata_json"`
	SourceJSON        string `json:"source_json"`
	UpdateCommand     string `json:"update_command"`
	UpdatePolicy      string `json:"update_policy"`
	AutoUpdate        int    `json:"auto_update"`
}

type sqliteReleaseRow struct {
	PackageName  string `json:"package_name"`
	VariantID    string `json:"variant_id"`
	Version      string `json:"version"`
	FileName     string `json:"file_name"`
	RelativePath string `json:"relative_path"`
	ContentType  string `json:"content_type"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	PublishedAt  string `json:"published_at"`
}

type sqlitePackageVersionRow struct {
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	Readme      string `json:"readme"`
	Notes       string `json:"notes"`
	UpdatedAt   string `json:"updated_at"`
}

type sqliteDistTagRow struct {
	PackageName string `json:"package_name"`
	Tag         string `json:"tag"`
	Version     string `json:"version"`
	UpdatedAt   string `json:"updated_at"`
}

type sqliteAliasRow struct {
	PackageName string `json:"package_name"`
	Alias       string `json:"alias"`
}

type packageBundle struct {
	Package  sqlitePackageRow
	Aliases  []string
	Variants []sqliteVariantRow
	Releases []sqliteReleaseRow
}

func packageRecordFromBundle(bundle packageBundle, baseURL string) PackageRecord {
	record := PackageRecord{
		Name:          bundle.Package.Name,
		Aliases:       append([]string(nil), bundle.Aliases...),
		Title:         bundle.Package.Title,
		Description:   bundle.Package.Description,
		Homepage:      bundle.Package.Homepage,
		LatestVersion: bundle.Package.LatestVersion,
		Variants:      make([]PackageVariant, 0, len(bundle.Variants)),
	}

	releaseByVariant := map[string]sqliteReleaseRow{}
	for _, release := range bundle.Releases {
		existing, ok := releaseByVariant[release.VariantID]
		if !ok || compareVersionStrings(release.Version, existing.Version) > 0 || (release.Version == existing.Version && release.PublishedAt > existing.PublishedAt) {
			releaseByVariant[release.VariantID] = release
		}
	}

	for _, row := range bundle.Variants {
		variant := packageVariantFromRow(row)
		if release, ok := releaseByVariant[row.VariantID]; ok {
			variant.Version = release.Version
			variant.FileName = release.FileName
			variant.SHA256 = release.SHA256
			if release.RelativePath != "" {
				variant.DownloadURL = joinURL(baseURL, fileURLPath(release.RelativePath))
			}
		}
		record.Variants = append(record.Variants, variant)
	}

	return record
}

func packageVariantFromRow(row sqliteVariantRow) PackageVariant {
	var notes []string
	if row.NotesJSON != "" {
		_ = json.Unmarshal([]byte(row.NotesJSON), &notes)
	}
	var metadata map[string]any
	if row.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(row.MetadataJSON), &metadata)
	}
	var source map[string]any
	if row.SourceJSON != "" {
		_ = json.Unmarshal([]byte(row.SourceJSON), &source)
	}

	return PackageVariant{
		ID:                row.VariantID,
		Label:             row.Label,
		OS:                row.OS,
		IsDefault:         row.IsDefault != 0,
		Default:           row.Default != 0,
		Version:           row.Version,
		Channel:           row.Channel,
		FileName:          row.FileName,
		DownloadURL:       row.DownloadURL,
		InstallCommand:    row.InstallCommand,
		SHA256:            row.SHA256,
		InstallStrategy:   row.InstallStrategy,
		UninstallStrategy: row.UninstallStrategy,
		InstallRoot:       row.InstallRoot,
		BinaryName:        row.BinaryName,
		WrapperName:       row.WrapperName,
		LauncherPath:      row.LauncherPath,
		Notes:             notes,
		Metadata:          metadata,
		Source:            source,
		UpdateCommand:     row.UpdateCommand,
		UpdatePolicy:      row.UpdatePolicy,
		AutoUpdate:        row.AutoUpdate != 0,
	}
}
