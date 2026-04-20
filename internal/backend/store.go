package backend

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"net/url"
	"path/filepath"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Perdonus/NV/internal/semver"
)

type Store struct {
	dataDir       string
	layoutVersion string
	dbPath        string
	artifactsDir  string
	mu            sync.Mutex
}

func OpenStore(cfg Config) (*Store, error) {
	dataDir := strings.TrimSpace(cfg.DataDir)
	if dataDir == "" {
		dataDir = filepath.Join(".", "var", "nvd")
	}

	layoutVersion := LayoutVersion
	layoutDir := filepath.Join(dataDir, layoutVersion)
	dbPath := filepath.Join(layoutDir, "nvd.sqlite")
	artifactsDir := strings.TrimSpace(cfg.FilesDir)
	if artifactsDir == "" {
		artifactsDir = filepath.Join(layoutDir, "files")
	}

	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		dataDir:       dataDir,
		layoutVersion: layoutVersion,
		dbPath:        dbPath,
		artifactsDir:  artifactsDir,
	}
	if err := store.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) DBPath() string { return s.dbPath }

func (s *Store) ArtifactsDir() string { return s.artifactsDir }

func (s *Store) LayoutVersion() string { return s.layoutVersion }

func (s *Store) ensureSchema(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	if existing >= SchemaVersion {
		return nil
	}

	return s.execScript(ctx, initSchemaSQL)
}

func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var rows []struct {
		Version int `json:"version"`
	}
	err := s.queryJSON(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;`, &rows)
	if err != nil {
		if isMissingTableError(err) {
			return 0, nil
		}
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Version, nil
}

func (s *Store) execScript(ctx context.Context, sql string) error {
	if strings.TrimSpace(sql) == "" {
		return nil
	}
	cmd := exec.CommandContext(
		ctx,
		"sqlite3",
		"-cmd", "PRAGMA foreign_keys=ON",
		"-cmd", "PRAGMA busy_timeout=5000",
		s.dbPath,
	)
	cmd.Stdin = strings.NewReader(sql + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite3 exec failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Store) queryJSON(ctx context.Context, sql string, dest any) error {
	cmd := exec.CommandContext(
		ctx,
		"sqlite3",
		"-json",
		"-cmd", "PRAGMA foreign_keys=ON",
		"-cmd", "PRAGMA busy_timeout=5000",
		s.dbPath,
	)
	cmd.Stdin = strings.NewReader(sql + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite3 query failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return json.Unmarshal(out, dest)
}

func (s *Store) seedCatalog(ctx context.Context, seedPath string) error {
	seedPath = strings.TrimSpace(seedPath)
	if seedPath == "" {
		return nil
	}

	data, err := os.ReadFile(seedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var catalog SeedCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return fmt.Errorf("invalid seed catalog: %w", err)
	}
	return s.upsertSeedCatalog(ctx, catalog)
}

func (s *Store) upsertSeedCatalog(ctx context.Context, catalog SeedCatalog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var script strings.Builder
	script.WriteString("BEGIN IMMEDIATE;\n")
	for _, pkg := range catalog.Packages {
		script.WriteString(s.upsertSeedPackageSQL(pkg))
	}
	script.WriteString("COMMIT;\n")
	return s.execScript(ctx, script.String())
}

func (s *Store) upsertSeedPackageSQL(pkg SeedPackage) string {
	name := canonicalPackageName(pkg.Name)
	aliases := normalizeAliases(pkg.Aliases)
	aliasesJSON := mustJSON(aliases)
	initialVersion := strings.TrimSpace(pkg.Version)
	now := time.Now().UTC().Format(time.RFC3339)

	var script strings.Builder
	script.WriteString(fmt.Sprintf(
		`INSERT INTO packages (name, title, description, homepage, aliases_json, latest_version, created_at, updated_at)
VALUES (%s, %s, %s, %s, %s, COALESCE(NULLIF((SELECT latest_version FROM packages WHERE name = %s), ''), %s), %s, %s)
ON CONFLICT(name) DO UPDATE SET
	title=excluded.title,
	description=excluded.description,
	homepage=excluded.homepage,
	aliases_json=excluded.aliases_json,
	updated_at=excluded.updated_at;
`,
		sqlQuote(name),
		sqlQuote(pkg.Title),
		sqlQuote(pkg.Description),
		sqlQuote(pkg.Homepage),
		sqlQuote(aliasesJSON),
		sqlQuote(name),
		sqlQuote(initialVersion),
		sqlQuote(now),
		sqlQuote(now),
	))

	script.WriteString(fmt.Sprintf("DELETE FROM package_aliases WHERE package_name=%s;\n", sqlQuote(name)))
	for _, alias := range aliases {
		script.WriteString(fmt.Sprintf(
			`INSERT INTO package_aliases (package_name, alias) VALUES (%s, %s)
ON CONFLICT(alias) DO UPDATE SET package_name=excluded.package_name;
`,
			sqlQuote(name),
			sqlQuote(alias),
		))
	}

	for _, variant := range pkg.Variants {
		row := seedVariantToRow(variant)
		script.WriteString(fmt.Sprintf(
			`INSERT INTO variants (
	package_name, variant_id, label, os, is_default, default_flag, version, channel, file_name, download_url,
	install_command, sha256, install_strategy, uninstall_strategy, install_root, binary_name, wrapper_name,
	launcher_path, notes_json, metadata_json, source_json, update_command, update_policy, auto_update
) VALUES (%s, %s, %s, %s, %d, %d, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d)
ON CONFLICT(package_name, variant_id) DO UPDATE SET
	label=excluded.label,
	os=excluded.os,
	is_default=excluded.is_default,
	default_flag=excluded.default_flag,
	version=excluded.version,
	channel=excluded.channel,
	file_name=excluded.file_name,
	download_url=excluded.download_url,
	install_command=excluded.install_command,
	sha256=excluded.sha256,
	install_strategy=excluded.install_strategy,
	uninstall_strategy=excluded.uninstall_strategy,
	install_root=excluded.install_root,
	binary_name=excluded.binary_name,
	wrapper_name=excluded.wrapper_name,
	launcher_path=excluded.launcher_path,
	notes_json=excluded.notes_json,
	metadata_json=excluded.metadata_json,
	source_json=excluded.source_json,
	update_command=excluded.update_command,
	update_policy=excluded.update_policy,
	auto_update=excluded.auto_update;
`,
			sqlQuote(name),
			sqlQuote(row.VariantID),
			sqlQuote(row.Label),
			sqlQuote(row.OS),
			row.IsDefault,
			row.DefaultFlag,
			sqlQuote(row.Version),
			sqlQuote(row.Channel),
			sqlQuote(row.FileName),
			sqlQuote(row.DownloadURL),
			sqlQuote(row.InstallCommand),
			sqlQuote(row.SHA256),
			sqlQuote(row.InstallStrategy),
			sqlQuote(row.UninstallStrategy),
			sqlQuote(row.InstallRoot),
			sqlQuote(row.BinaryName),
			sqlQuote(row.WrapperName),
			sqlQuote(row.LauncherPath),
			sqlQuote(mustJSON(row.Notes)),
			sqlQuote(mustObjectJSON(row.Metadata)),
			sqlQuote(mustObjectJSON(row.Source)),
			sqlQuote(row.UpdateCommand),
			sqlQuote(row.UpdatePolicy),
			row.AutoUpdate,
		))
	}
	return script.String()
}

type seedVariantRow struct {
	VariantID         string
	Label             string
	OS                string
	IsDefault         int
	DefaultFlag       int
	Version           string
	Channel           string
	FileName          string
	DownloadURL       string
	InstallCommand    string
	SHA256            string
	InstallStrategy   string
	UninstallStrategy string
	InstallRoot       string
	BinaryName        string
	WrapperName       string
	LauncherPath      string
	Notes             []string
	Metadata          map[string]any
	Source            map[string]any
	UpdateCommand     string
	UpdatePolicy      string
	AutoUpdate        int
}

func seedVariantToRow(variant SeedVariant) seedVariantRow {
	return seedVariantRow{
		VariantID:         strings.TrimSpace(variant.ID),
		Label:             strings.TrimSpace(variant.Label),
		OS:                normalizeOS(variant.OS),
		IsDefault:         boolToInt(variant.IsDefault),
		DefaultFlag:       boolToInt(variant.Default),
		Version:           strings.TrimSpace(variant.Version),
		Channel:           strings.TrimSpace(variant.Channel),
		FileName:          strings.TrimSpace(variant.FileName),
		DownloadURL:       strings.TrimSpace(variant.DownloadURL),
		InstallCommand:    strings.TrimSpace(variant.InstallCommand),
		SHA256:            strings.TrimSpace(variant.SHA256),
		InstallStrategy:   strings.TrimSpace(variant.InstallStrategy),
		UninstallStrategy: strings.TrimSpace(variant.UninstallStrategy),
		InstallRoot:       strings.TrimSpace(variant.InstallRoot),
		BinaryName:        strings.TrimSpace(variant.BinaryName),
		WrapperName:       strings.TrimSpace(variant.WrapperName),
		LauncherPath:      strings.TrimSpace(variant.LauncherPath),
		Notes:             append([]string(nil), variant.Notes...),
		Metadata:          variant.Metadata,
		Source:            variant.Source,
		UpdateCommand:     strings.TrimSpace(variant.UpdateCommand),
		UpdatePolicy:      strings.TrimSpace(variant.UpdatePolicy),
		AutoUpdate:        boolToInt(variant.AutoUpdate),
	}
}

func (s *Store) LoadCatalog(ctx context.Context, baseURL string) ([]packageBundle, error) {
	var packages []sqlitePackageRow
	if err := s.queryJSON(ctx, `SELECT name, title, description, homepage, latest_version, aliases_json FROM packages ORDER BY name;`, &packages); err != nil {
		return nil, err
	}

	var variants []sqliteVariantRow
	if err := s.queryJSON(ctx, `SELECT package_name, variant_id, label, os, is_default, default_flag, version, channel, file_name, download_url,
	install_command, sha256, install_strategy, uninstall_strategy, install_root, binary_name, wrapper_name, launcher_path,
	notes_json, metadata_json, source_json, update_command, update_policy, auto_update
	FROM variants ORDER BY package_name, variant_id;`, &variants); err != nil {
		return nil, err
	}

	var releases []sqliteReleaseRow
	if err := s.queryJSON(ctx, `SELECT package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
	FROM releases ORDER BY package_name, variant_id, published_at DESC, version DESC;`, &releases); err != nil {
		return nil, err
	}

	var aliasRows []sqliteAliasRow
	if err := s.queryJSON(ctx, `SELECT package_name, alias FROM package_aliases ORDER BY package_name, alias;`, &aliasRows); err != nil {
		return nil, err
	}

	aliasesByPackage := map[string][]string{}
	for _, row := range aliasRows {
		aliasesByPackage[row.PackageName] = append(aliasesByPackage[row.PackageName], row.Alias)
	}

	variantByPackage := map[string][]sqliteVariantRow{}
	for _, row := range variants {
		variantByPackage[row.PackageName] = append(variantByPackage[row.PackageName], row)
	}

	releaseByPackage := map[string][]sqliteReleaseRow{}
	for _, row := range releases {
		releaseByPackage[row.PackageName] = append(releaseByPackage[row.PackageName], row)
	}

	bundles := make([]packageBundle, 0, len(packages))
	for _, pkg := range packages {
		bundles = append(bundles, packageBundle{
			Package:  pkg,
			Aliases:  append([]string(nil), aliasesByPackage[pkg.Name]...),
			Variants: append([]sqliteVariantRow(nil), variantByPackage[pkg.Name]...),
			Releases: append([]sqliteReleaseRow(nil), releaseByPackage[pkg.Name]...),
		})
	}
	return bundles, nil
}

func (s *Store) Publish(ctx context.Context, req PublishRequest, artifact io.Reader) (PublishResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := canonicalPackageName(strings.TrimSpace(req.Name))
	if name == "" {
		return PublishResult{}, errors.New("package name is required")
	}
	version, err := semver.Normalize(strings.TrimSpace(req.Version))
	if err != nil {
		return PublishResult{}, fmt.Errorf("invalid version: %w", err)
	}

	var pkg SeedPackage
	if req.Package != nil {
		pkg = *req.Package
		if strings.TrimSpace(pkg.Name) == "" {
			pkg.Name = name
		}
		pkg.Name = canonicalPackageName(pkg.Name)
	} else {
		if existing, err := s.findPackage(ctx, name); err == nil {
			bundle, bundleErr := s.loadBundle(ctx, existing.Name)
			if bundleErr == nil {
				pkg = seedPackageFromBundle(bundle)
			}
		}
		if strings.TrimSpace(pkg.Name) == "" {
			pkg = SeedPackage{Name: name}
		}
	}
	pkg.Name = canonicalPackageName(pkg.Name)

	variantID := strings.TrimSpace(req.Variant)
	if variantID == "" {
		switch {
		case len(pkg.Variants) == 1:
			variantID = strings.TrimSpace(pkg.Variants[0].ID)
		default:
			for _, variant := range pkg.Variants {
				if variant.IsDefault || variant.Default {
					variantID = strings.TrimSpace(variant.ID)
					break
				}
			}
		}
	}
	if variantID == "" {
		variantID = "default"
	}
	pkg.Variants = ensureVariantForPublish(pkg.Variants, variantID)

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = fmt.Sprintf("%s-%s.bin", safePathSegment(pkg.Name), safePathSegment(version))
	}
	fileName = filepath.Base(fileName)

	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	packageDir := filepath.Join(s.artifactsDir, safePathSegment(pkg.Name), safePathSegment(variantID), safePathSegment(version))
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return PublishResult{}, err
	}
	relativePath := pathJoin(
		safePathSegment(pkg.Name),
		safePathSegment(variantID),
		safePathSegment(version),
		fileName,
	)
	artifactPath := filepath.Join(s.artifactsDir, filepath.FromSlash(relativePath))

	tempFile, err := os.CreateTemp(packageDir, ".upload-*")
	if err != nil {
		return PublishResult{}, err
	}
	tempPath := tempFile.Name()

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tempFile, hash), artifact)
	closeErr := tempFile.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return PublishResult{}, err
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return PublishResult{}, closeErr
	}
	if err := os.Rename(tempPath, artifactPath); err != nil {
		_ = os.Remove(tempPath)
		return PublishResult{}, err
	}

	sha256Hex := hex.EncodeToString(hash.Sum(nil))
	now := time.Now().UTC().Format(time.RFC3339)
	script := strings.Builder{}
	script.WriteString("BEGIN IMMEDIATE;\n")
	script.WriteString(s.upsertSeedPackageSQL(pkg))
	script.WriteString(fmt.Sprintf(
		`INSERT INTO releases (
	package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
) VALUES (%s, %s, %s, %s, %s, %s, %s, %d, %s)
ON CONFLICT(package_name, variant_id, version) DO UPDATE SET
	file_name=excluded.file_name,
	relative_path=excluded.relative_path,
	content_type=excluded.content_type,
	sha256=excluded.sha256,
	size=excluded.size,
	published_at=excluded.published_at;
`,
		sqlQuote(pkg.Name),
		sqlQuote(variantID),
		sqlQuote(version),
		sqlQuote(fileName),
		sqlQuote(relativePath),
		sqlQuote(contentType),
		sqlQuote(sha256Hex),
		size,
		sqlQuote(now),
	))
	script.WriteString("COMMIT;\n")

	if err := s.execScript(ctx, script.String()); err != nil {
		_ = os.Remove(artifactPath)
		return PublishResult{}, err
	}

	record, err := s.findPackage(ctx, pkg.Name)
	if err != nil {
		return PublishResult{}, err
	}
	bundle, err := s.loadBundle(ctx, record.Name)
	if err != nil {
		return PublishResult{}, err
	}
	resultRecord := packageRecordFromBundle(bundle, "")
	latestVersion := resultRecord.LatestVersion
	if latestVersion == "" {
		latestVersion, _ = s.latestVersionForPackage(ctx, resultRecord.Name)
	}
	if latestVersion != "" {
		_ = s.execScript(ctx, fmt.Sprintf(`UPDATE packages SET latest_version = %s, updated_at = %s WHERE name = %s;`, sqlQuote(latestVersion), sqlQuote(now), sqlQuote(resultRecord.Name)))
		resultRecord.LatestVersion = latestVersion
	}
	resultURL := fileURLPath(relativePath)
	return PublishResult{
		PackageRecord:   resultRecord,
		ResolvedVersion: version,
		VariantID:       variantID,
		FileName:        fileName,
		DownloadURL:     resultURL,
		RelativePath:    relativePath,
		SHA256:          sha256Hex,
		ContentType:     contentType,
		Size:            size,
	}, nil
}

func ensureVariantForPublish(variants []SeedVariant, variantID string) []SeedVariant {
	for _, variant := range variants {
		if strings.EqualFold(strings.TrimSpace(variant.ID), variantID) {
			return variants
		}
	}
	return append(variants, SeedVariant{ID: variantID})
}

func seedPackageFromBundle(bundle packageBundle) SeedPackage {
	pkg := SeedPackage{
		Name:        bundle.Package.Name,
		Title:       bundle.Package.Title,
		Description: bundle.Package.Description,
		Homepage:    bundle.Package.Homepage,
		Aliases:     append([]string(nil), bundle.Aliases...),
		Variants:    make([]SeedVariant, 0, len(bundle.Variants)),
	}
	for _, variant := range bundle.Variants {
		pkg.Variants = append(pkg.Variants, seedVariantFromRow(variant))
	}
	return pkg
}

func seedVariantFromRow(row sqliteVariantRow) SeedVariant {
	var notes []string
	_ = json.Unmarshal([]byte(row.NotesJSON), &notes)
	var metadata map[string]any
	_ = json.Unmarshal([]byte(row.MetadataJSON), &metadata)
	var source map[string]any
	_ = json.Unmarshal([]byte(row.SourceJSON), &source)
	return SeedVariant{
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

func (s *Store) findPackage(ctx context.Context, name string) (sqlitePackageRow, error) {
	var rows []sqlitePackageRow
	canonical := canonicalPackageName(name)
	query := fmt.Sprintf(`SELECT name, title, description, homepage, latest_version, aliases_json
FROM packages
WHERE name = %s OR name IN (SELECT package_name FROM package_aliases WHERE alias = %s)
LIMIT 1;`, sqlQuote(canonical), sqlQuote(strings.ToLower(strings.TrimSpace(name))))
	if err := s.queryJSON(ctx, query, &rows); err != nil {
		return sqlitePackageRow{}, err
	}
	if len(rows) == 0 {
		return sqlitePackageRow{}, fmt.Errorf("package %q not found", name)
	}
	return rows[0], nil
}

func (s *Store) loadBundle(ctx context.Context, packageName string) (packageBundle, error) {
	var packages []sqlitePackageRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT name, title, description, homepage, latest_version, aliases_json
FROM packages WHERE name = %s LIMIT 1;`, sqlQuote(packageName)), &packages); err != nil {
		return packageBundle{}, err
	}
	if len(packages) == 0 {
		return packageBundle{}, fmt.Errorf("package %q not found", packageName)
	}

	var variants []sqliteVariantRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, variant_id, label, os, is_default, default_flag, version, channel, file_name, download_url,
	install_command, sha256, install_strategy, uninstall_strategy, install_root, binary_name, wrapper_name, launcher_path,
	notes_json, metadata_json, source_json, update_command, update_policy, auto_update
FROM variants WHERE package_name = %s ORDER BY variant_id;`, sqlQuote(packageName)), &variants); err != nil {
		return packageBundle{}, err
	}

	var releases []sqliteReleaseRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
FROM releases WHERE package_name = %s ORDER BY published_at DESC, version DESC;`, sqlQuote(packageName)), &releases); err != nil {
		return packageBundle{}, err
	}

	var aliasRows []sqliteAliasRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, alias FROM package_aliases WHERE package_name = %s ORDER BY alias;`, sqlQuote(packageName)), &aliasRows); err != nil {
		return packageBundle{}, err
	}

	aliases := make([]string, 0, len(aliasRows))
	for _, row := range aliasRows {
		aliases = append(aliases, row.Alias)
	}

	return packageBundle{
		Package:  packages[0],
		Aliases:  aliases,
		Variants: variants,
		Releases: releases,
	}, nil
}

func (s *Store) VersionMetadata(ctx context.Context, packageName, version string) (sqlitePackageVersionRow, error) {
	var rows []sqlitePackageVersionRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, version, readme, notes, updated_at
FROM package_versions WHERE package_name = %s AND version = %s LIMIT 1;`, sqlQuote(packageName), sqlQuote(version)), &rows); err != nil {
		return sqlitePackageVersionRow{}, err
	}
	if len(rows) == 0 {
		return sqlitePackageVersionRow{}, os.ErrNotExist
	}
	return rows[0], nil
}

func (s *Store) DistTagsForPackage(ctx context.Context, packageName string) (map[string]string, error) {
	var rows []sqliteDistTagRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, tag, version, updated_at
FROM package_dist_tags WHERE package_name = %s ORDER BY tag;`, sqlQuote(packageName)), &rows); err != nil {
		if isMissingTableError(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	result := map[string]string{}
	for _, row := range rows {
		tag := strings.TrimSpace(row.Tag)
		version := strings.TrimSpace(row.Version)
		if tag == "" || version == "" {
			continue
		}
		result[tag] = version
	}
	if len(result) == 0 {
		if pkg, err := s.findPackage(ctx, packageName); err == nil && strings.TrimSpace(pkg.LatestVersion) != "" {
			result["latest"] = strings.TrimSpace(pkg.LatestVersion)
		}
	}
	return result, nil
}

func (s *Store) SaveVersionMetadata(ctx context.Context, packageName, version, readme, notes string, distTags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	packageName = canonicalPackageName(packageName)
	version = strings.TrimSpace(version)
	if packageName == "" || version == "" {
		return errors.New("package name and version are required")
	}

	distTags = normalizeDistTags(distTags)
	now := time.Now().UTC().Format(time.RFC3339)
	latestVersion := ""
	if existing, err := s.findPackage(ctx, packageName); err == nil {
		latestVersion = strings.TrimSpace(existing.LatestVersion)
	}
	if len(distTags) == 0 {
		distTags = []string{"latest"}
	}
	for _, tag := range distTags {
		if tag == "latest" {
			latestVersion = version
			break
		}
	}
	if latestVersion == "" {
		latestVersion = version
	}

	var script strings.Builder
	script.WriteString("BEGIN IMMEDIATE;\n")
	script.WriteString(fmt.Sprintf(
		`INSERT INTO package_versions (package_name, version, readme, notes, created_at, updated_at)
VALUES (%s, %s, %s, %s, %s, %s)
ON CONFLICT(package_name, version) DO UPDATE SET
	readme=excluded.readme,
	notes=excluded.notes,
	updated_at=excluded.updated_at;
`,
		sqlQuote(packageName),
		sqlQuote(version),
		sqlQuote(strings.TrimSpace(readme)),
		sqlQuote(strings.TrimSpace(notes)),
		sqlQuote(now),
		sqlQuote(now),
	))
	for _, tag := range distTags {
		script.WriteString(fmt.Sprintf(
			`INSERT INTO package_dist_tags (package_name, tag, version, updated_at)
VALUES (%s, %s, %s, %s)
ON CONFLICT(package_name, tag) DO UPDATE SET
	version=excluded.version,
	updated_at=excluded.updated_at;
`,
			sqlQuote(packageName),
			sqlQuote(tag),
			sqlQuote(version),
			sqlQuote(now),
		))
	}
	script.WriteString(fmt.Sprintf(
		`UPDATE packages SET latest_version = %s, updated_at = %s WHERE name = %s;
`,
		sqlQuote(latestVersion),
		sqlQuote(now),
		sqlQuote(packageName),
	))
	script.WriteString("COMMIT;\n")
	return s.execScript(ctx, script.String())
}

func (s *Store) packageBundles(ctx context.Context, baseURL string) ([]packageBundle, error) {
	return s.LoadCatalog(ctx, baseURL)
}

func (s *Store) latestVersionForPackage(ctx context.Context, packageName string) (string, error) {
	var rows []sqliteReleaseRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
FROM releases WHERE package_name = %s;`, sqlQuote(packageName)), &rows); err != nil {
		return "", err
	}
	latest := ""
	for _, row := range rows {
		if compareVersionStrings(row.Version, latest) > 0 {
			latest = row.Version
		}
	}
	return latest, nil
}

func (s *Store) releaseRows(ctx context.Context, packageName string) ([]sqliteReleaseRow, error) {
	var rows []sqliteReleaseRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
FROM releases WHERE package_name = %s ORDER BY published_at DESC, version DESC;`, sqlQuote(packageName)), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) releaseByPath(ctx context.Context, relativePath string) (sqliteReleaseRow, error) {
	var rows []sqliteReleaseRow
	if err := s.queryJSON(ctx, fmt.Sprintf(`SELECT package_name, variant_id, version, file_name, relative_path, content_type, sha256, size, published_at
FROM releases WHERE relative_path = %s LIMIT 1;`, sqlQuote(relativePath)), &rows); err != nil {
		return sqliteReleaseRow{}, err
	}
	if len(rows) == 0 {
		return sqliteReleaseRow{}, os.ErrNotExist
	}
	return rows[0], nil
}

func (s *Store) openArtifact(relativePath string) (string, error) {
	relativePath = path.Clean("/" + strings.ReplaceAll(relativePath, "\\", "/"))
	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(s.artifactsDir, filepath.FromSlash(relativePath))
	normalizedRoot := filepath.Clean(s.artifactsDir)
	normalizedPath := filepath.Clean(fullPath)
	rel, err := filepath.Rel(normalizedRoot, normalizedPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("invalid artifact path")
	}
	return fullPath, nil
}

func (s *Store) readArtifact(relativePath string) (*os.File, error) {
	fullPath, err := s.openArtifact(relativePath)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (s *Store) deleteArtifact(relativePath string) error {
	fullPath, err := s.openArtifact(relativePath)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func isMissingTableError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func sqlQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func mustJSON(value any) string {
	if value == nil {
		return "[]"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

func mustObjectJSON(value any) string {
	if value == nil {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	if string(data) == "null" {
		return "{}"
	}
	return string(data)
}

func normalizeAliases(values []string) []string {
	seen := map[string]struct{}{}
	aliases := make([]string, 0, len(values))
	for _, value := range values {
		alias := strings.ToLower(strings.TrimSpace(value))
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func normalizeDistTags(values []string) []string {
	seen := map[string]struct{}{}
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.ToLower(strings.TrimSpace(value))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func canonicalPackageName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "neuralv", "@lvls/neuralv":
		return "neuralv"
	case "nv", "@lvls/nv":
		return "nv"
	default:
		return normalized
	}
}

func normalizeOS(goos string) string {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "windows", "linux":
		return strings.ToLower(strings.TrimSpace(goos))
	case "win32":
		return "windows"
	default:
		return strings.ToLower(strings.TrimSpace(goos))
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, "\\", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.ReplaceAll(value, "..", "-")
	return value
}

func pathJoin(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	return filepath.ToSlash(filepath.Join(cleaned...))
}

func compareVersionStrings(left, right string) int {
	if strings.TrimSpace(right) == "" {
		return 1
	}
	if strings.TrimSpace(left) == "" {
		return -1
	}
	return semver.Compare(left, right)
}

func joinURL(base, rel string) string {
	rel = "/" + strings.TrimPrefix(rel, "/")
	base = strings.TrimSpace(base)
	if base == "" {
		return rel
	}
	return strings.TrimRight(base, "/") + rel
}

func fileURLPath(relativePath string) string {
	parts := strings.Split(strings.TrimPrefix(filepath.ToSlash(relativePath), "/"), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return "/files/" + strings.Join(parts, "/")
}

func decodeBase64Bytes(raw string) ([]byte, error) {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimSpace(raw)))
	return io.ReadAll(reader)
}

const initSchemaSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS packages (
  name TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  homepage TEXT NOT NULL DEFAULT '',
  aliases_json TEXT NOT NULL DEFAULT '[]',
  latest_version TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS package_aliases (
  alias TEXT PRIMARY KEY,
  package_name TEXT NOT NULL,
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS variants (
  package_name TEXT NOT NULL,
  variant_id TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  default_flag INTEGER NOT NULL DEFAULT 0,
  version TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  download_url TEXT NOT NULL DEFAULT '',
  install_command TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  install_strategy TEXT NOT NULL DEFAULT '',
  uninstall_strategy TEXT NOT NULL DEFAULT '',
  install_root TEXT NOT NULL DEFAULT '',
  binary_name TEXT NOT NULL DEFAULT '',
  wrapper_name TEXT NOT NULL DEFAULT '',
  launcher_path TEXT NOT NULL DEFAULT '',
  notes_json TEXT NOT NULL DEFAULT '[]',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  source_json TEXT NOT NULL DEFAULT '{}',
  update_command TEXT NOT NULL DEFAULT '',
  update_policy TEXT NOT NULL DEFAULT '',
  auto_update INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (package_name, variant_id),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS releases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  package_name TEXT NOT NULL,
  variant_id TEXT NOT NULL,
  version TEXT NOT NULL,
  file_name TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  published_at TEXT NOT NULL,
  UNIQUE(package_name, variant_id, version),
  FOREIGN KEY(package_name, variant_id) REFERENCES variants(package_name, variant_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS package_versions (
  package_name TEXT NOT NULL,
  version TEXT NOT NULL,
  readme TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (package_name, version),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS package_dist_tags (
  package_name TEXT NOT NULL,
  tag TEXT NOT NULL,
  version TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (package_name, tag),
  FOREIGN KEY(package_name) REFERENCES packages(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_variants_package ON variants(package_name, variant_id);
CREATE INDEX IF NOT EXISTS idx_releases_package ON releases(package_name, version DESC, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_package_variant ON releases(package_name, variant_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_package_versions_package ON package_versions(package_name, version DESC);
CREATE INDEX IF NOT EXISTS idx_package_dist_tags_package ON package_dist_tags(package_name, version);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
`
