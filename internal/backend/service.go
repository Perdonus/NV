package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store         *Store
	publicBaseURL string
	layoutVersion string
	publishToken  string
}

func NewService(cfg Config) (*Service, error) {
	store, err := OpenStore(cfg)
	if err != nil {
		return nil, err
	}
	if err := store.seedCatalog(context.Background(), cfg.SeedPath); err != nil {
		return nil, err
	}
	return &Service{
		store:         store,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		layoutVersion: store.LayoutVersion(),
		publishToken:  strings.TrimSpace(cfg.PublishToken),
	}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/whoami", s.handleWhoami)
	mux.HandleFunc("/api/whoami", s.handleWhoami)
	mux.HandleFunc("/packages", s.handlePackages)
	mux.HandleFunc("/api/packages", s.handlePackages)
	mux.HandleFunc("/packages/details", s.handlePackageDetails)
	mux.HandleFunc("/api/packages/details", s.handlePackageDetails)
	mux.HandleFunc("/packages/resolve", s.handlePackageResolve)
	mux.HandleFunc("/api/packages/resolve", s.handlePackageResolve)
	mux.HandleFunc("/packages/view", s.handlePackageView)
	mux.HandleFunc("/api/packages/view", s.handlePackageView)
	mux.HandleFunc("/publish", s.handlePublish)
	mux.HandleFunc("/api/publish", s.handlePublish)
	mux.HandleFunc("/bootstrap/manifest", s.handleBootstrapManifest)
	mux.HandleFunc("/api/bootstrap/manifest", s.handleBootstrapManifest)
	mux.HandleFunc("/files/", s.handleFiles)
	return mux
}

func (s *Service) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	s.handleHealthz(w, r)
}

func (s *Service) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Service) handleWhoami(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid publish token"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"identity": "publisher",
	})
}

func (s *Service) handlePackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	catalog, err := s.loadCatalog(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := struct {
		Success  bool            `json:"success"`
		Packages []PackageRecord `json:"packages"`
		Error    string          `json:"error,omitempty"`
	}{
		Success:  true,
		Packages: make([]PackageRecord, 0, len(catalog)),
	}
	requestedOS := normalizeOS(r.URL.Query().Get("os"))
	for _, bundle := range catalog {
		record := filterPackageRecordByOS(packageRecordFromBundle(bundle, s.baseURL(r)), requestedOS)
		if requestedOS != "" && requestedOS != "all" && len(record.Variants) == 0 {
			continue
		}
		response.Packages = append(response.Packages, record)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePackageDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	bundle, err := s.packageBundleByName(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	response := struct {
		Success bool          `json:"success"`
		Package PackageRecord `json:"package"`
		Error   string        `json:"error,omitempty"`
	}{
		Success: true,
		Package: filterPackageRecordByOS(packageRecordFromBundle(bundle, s.baseURL(r)), normalizeOS(r.URL.Query().Get("os"))),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePackageResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	variantID := strings.TrimSpace(r.URL.Query().Get("variant"))
	goos := normalizeOS(r.URL.Query().Get("os"))

	bundle, err := s.packageBundleByName(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	requestedOS := normalizeOS(r.URL.Query().Get("os"))
	record := filterPackageRecordByOS(packageRecordFromBundle(bundle, s.baseURL(r)), requestedOS)
	distTags, err := s.store.DistTagsForPackage(r.Context(), record.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resolved, err := resolvePackage(bundle, record, resolveRequestedVersion(version, record.LatestVersion, distTags), goos, variantID, s.baseURL(r))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	response := struct {
		Success bool            `json:"success"`
		Package ResolvedPackage `json:"package"`
		Error   string          `json:"error,omitempty"`
	}{
		Success: true,
		Package: resolved,
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePackageView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	bundle, err := s.packageBundleByName(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	requestedOS := normalizeOS(r.URL.Query().Get("os"))
	record := filterPackageRecordByOS(packageRecordFromBundle(bundle, s.baseURL(r)), requestedOS)
	distTags, err := s.store.DistTagsForPackage(r.Context(), record.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	selectedVersion := resolveRequestedVersion(strings.TrimSpace(r.URL.Query().Get("version")), record.LatestVersion, distTags)
	versions := knownVersions(record, bundle.Releases)
	versionMeta, err := s.store.VersionMetadata(r.Context(), record.Name, selectedVersion)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	versionView := packageVersionView(bundle, record, selectedVersion, tagsForVersion(distTags, selectedVersion), strings.TrimSpace(versionMeta.Readme), s.baseURL(r))
	view := PackageView{
		PackageRecord: record,
		DistTags:      distTags,
		Versions:      versions,
		Version:       versionView,
		Releases:      make([]PackageRelease, 0, len(bundle.Releases)),
	}
	for _, release := range bundle.Releases {
		view.Releases = append(view.Releases, releaseFromRow(release, s.baseURL(r)))
	}
	sort.SliceStable(view.Releases, func(i, j int) bool {
		if view.Releases[i].Version != view.Releases[j].Version {
			return compareVersionStrings(view.Releases[i].Version, view.Releases[j].Version) > 0
		}
		return view.Releases[i].PublishedAt.After(view.Releases[j].PublishedAt)
	})
	response := struct {
		Success bool        `json:"success"`
		Package PackageView `json:"package"`
		Error   string      `json:"error,omitempty"`
	}{
		Success: true,
		Package: view,
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleBootstrapManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	catalog, err := s.loadCatalog(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if platform := strings.TrimSpace(r.URL.Query().Get("platform")); platform != "" {
		type bootstrapArtifact struct {
			Platform    string         `json:"platform"`
			Version     string         `json:"version"`
			Channel     string         `json:"channel,omitempty"`
			FileName    string         `json:"file_name,omitempty"`
			DownloadURL string         `json:"download_url,omitempty"`
			SHA256      string         `json:"sha256,omitempty"`
			Metadata    map[string]any `json:"metadata,omitempty"`
		}
		response := struct {
			Success     bool                `json:"success"`
			GeneratedAt time.Time           `json:"generated_at"`
			Artifacts   []bootstrapArtifact `json:"artifacts"`
			Error       string              `json:"error,omitempty"`
		}{
			Success:     true,
			GeneratedAt: time.Now().UTC(),
			Artifacts:   []bootstrapArtifact{},
		}
		for _, bundle := range catalog {
			record := packageRecordFromBundle(bundle, s.baseURL(r))
			for _, variant := range record.Variants {
				if !strings.EqualFold(variant.ID, platform) && !strings.EqualFold(variant.OS, platform) {
					continue
				}
				artifact := bootstrapArtifact{
					Platform: variant.ID,
					Version:  chooseString(variant.Version, record.LatestVersion),
					Channel:  variant.Channel,
					FileName: variant.FileName,
					SHA256:   variant.SHA256,
					Metadata: map[string]any{
						"package": record.Name,
					},
				}
				if release, ok := releaseForVariantAndVersion(bundle.Releases, variant.ID, record.LatestVersion); ok {
					artifact.Version = chooseString(release.Version, artifact.Version)
					artifact.FileName = chooseString(release.FileName, artifact.FileName)
					artifact.DownloadURL = joinURL(s.baseURL(r), fileURLPath(release.RelativePath))
					artifact.SHA256 = chooseString(release.SHA256, artifact.SHA256)
				} else {
					artifact.DownloadURL = absoluteDownloadURL(s.baseURL(r), variant.DownloadURL)
				}
				if strings.TrimSpace(artifact.DownloadURL) == "" {
					continue
				}
				response.Artifacts = append(response.Artifacts, artifact)
			}
		}
		if len(response.Artifacts) == 0 {
			writeError(w, http.StatusNotFound, fmt.Errorf("platform %s not found", platform))
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	manifest := BootstrapManifest{
		LayoutVersion: s.layoutVersion,
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		BaseURL:       s.baseURL(r),
		Packages:      make([]BootstrapPackage, 0, len(catalog)),
	}
	for _, bundle := range catalog {
		record := packageRecordFromBundle(bundle, s.baseURL(r))
		bootstrap := BootstrapPackage{
			Name:          record.Name,
			Aliases:       append([]string(nil), record.Aliases...),
			Title:         record.Title,
			Description:   record.Description,
			Homepage:      record.Homepage,
			LatestVersion: record.LatestVersion,
			ViewURL:       s.joinPath(r, "/packages/view?name="+url.QueryEscape(record.Name)),
			DetailsURL:    s.joinPath(r, "/packages/details?name="+url.QueryEscape(record.Name)),
			ResolveURL:    s.joinPath(r, "/packages/resolve?name="+url.QueryEscape(record.Name)),
			Variants:      make([]BootstrapVariant, 0, len(record.Variants)),
		}
		for _, variant := range record.Variants {
			bootstrap.Variants = append(bootstrap.Variants, BootstrapVariant{
				ID:            variant.ID,
				Label:         variant.Label,
				OS:            variant.OS,
				Default:       variant.Default,
				IsDefault:     variant.IsDefault,
				LatestVersion: variant.Version,
				DownloadURL:   variant.DownloadURL,
			})
		}
		manifest.Packages = append(manifest.Packages, bootstrap)
	}

	response := struct {
		Success bool              `json:"success"`
		Manifest BootstrapManifest `json:"manifest"`
		Error   string            `json:"error,omitempty"`
	}{
		Success: true,
		Manifest: manifest,
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid publish token"))
		return
	}
	payload, cleanup, err := s.parsePublishRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer cleanup()

	results := make([]PublishResult, 0, len(payload.Artifacts))
	dryRun := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("dry_run")), "true")
	for _, variant := range payload.Manifest.Variants {
		part, ok := payload.Artifacts[variant.ID]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Errorf("artifact:%s is required", variant.ID))
			return
		}
		if dryRun {
			continue
		}
		result, err := s.store.Publish(r.Context(), PublishRequest{
			Name:        payload.Manifest.Name,
			Version:     payload.Manifest.Version,
			Variant:     variant.ID,
			FileName:    chooseString(variant.FileName, part.FileName),
			ContentType: part.ContentType,
			Package:     &payload.Manifest,
		}, bytes.NewReader(part.Payload))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result.DownloadURL = joinURL(s.baseURL(r), result.DownloadURL)
		results = append(results, result)
	}

	if !dryRun {
		if err := s.store.SaveVersionMetadata(r.Context(), payload.Manifest.Name, payload.Manifest.Version, payload.Readme, payload.Notes, payload.Manifest.DistTags); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	record := PackageRecord{
		Name:          payload.Manifest.Name,
		Aliases:       append([]string(nil), payload.Manifest.Aliases...),
		Title:         payload.Manifest.Title,
		Description:   payload.Manifest.Description,
		Homepage:      payload.Manifest.Homepage,
		LatestVersion: payload.Manifest.Version,
		Variants:      make([]PackageVariant, 0, len(payload.Manifest.Variants)),
	}
	for _, variant := range payload.Manifest.Variants {
		fileName := strings.TrimSpace(variant.FileName)
		downloadURL := ""
		if artifact, ok := payload.Artifacts[variant.ID]; ok {
			if fileName == "" {
				fileName = artifact.FileName
			}
		}
		if !dryRun {
			for _, result := range results {
				if strings.EqualFold(result.VariantID, variant.ID) {
					downloadURL = result.DownloadURL
					break
				}
			}
		}
		record.Variants = append(record.Variants, PackageVariant{
			ID:                variant.ID,
			Label:             variant.Label,
			OS:                variant.OS,
			Default:           variant.Default,
			IsDefault:         variant.IsDefault,
			Version:           payload.Manifest.Version,
			FileName:          fileName,
			DownloadURL:       downloadURL,
			InstallStrategy:   variant.InstallStrategy,
			UninstallStrategy: variant.UninstallStrategy,
			InstallRoot:       variant.InstallRoot,
			BinaryName:        variant.BinaryName,
			WrapperName:       variant.WrapperName,
			LauncherPath:      variant.LauncherPath,
			Metadata:          variant.Metadata,
		})
	}
	versionView := PackageVersionView{
		Version:     payload.Manifest.Version,
		Description: payload.Manifest.Description,
		Homepage:    payload.Manifest.Homepage,
		Readme:      payload.Readme,
		Variants:    append([]PackageVariant(nil), record.Variants...),
		DistTags:    tagsForVersion(distTagMapFromSlice(payload.Manifest.DistTags, payload.Manifest.Version), payload.Manifest.Version),
	}
	if !dryRun {
		bundle, err := s.packageBundleByName(r.Context(), payload.Manifest.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		record = packageRecordFromBundle(bundle, s.baseURL(r))
		record = filterPackageRecordByOS(record, "all")
		distTags, err := s.store.DistTagsForPackage(r.Context(), record.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		versionMeta, err := s.store.VersionMetadata(r.Context(), record.Name, payload.Manifest.Version)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		versionView = packageVersionView(bundle, record, payload.Manifest.Version, tagsForVersion(distTags, payload.Manifest.Version), strings.TrimSpace(versionMeta.Readme), s.baseURL(r))
	}

	response := struct {
		Success bool `json:"success"`
		Package struct {
			Name          string            `json:"name"`
			Title         string            `json:"title"`
			Description   string            `json:"description"`
			Homepage      string            `json:"homepage"`
			LatestVersion string            `json:"latest_version"`
			DistTags      map[string]string `json:"dist_tags"`
			Version       PackageVersionView `json:"version"`
		} `json:"package"`
		Error string `json:"error,omitempty"`
	}{
		Success: true,
	}
	response.Package.Name = record.Name
	response.Package.Title = record.Title
	response.Package.Description = record.Description
	response.Package.Homepage = record.Homepage
	response.Package.LatestVersion = record.LatestVersion
	response.Package.DistTags = distTagMapFromSlice(payload.Manifest.DistTags, payload.Manifest.Version)
	if !dryRun {
		if persistedTags, err := s.store.DistTagsForPackage(r.Context(), record.Name); err == nil && len(persistedTags) > 0 {
			response.Package.DistTags = persistedTags
		}
	}
	response.Package.Version = versionView
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/files/")
	relative = path.Clean("/" + relative)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" || relative == "." {
		http.NotFound(w, r)
		return
	}

	file, err := s.store.readArtifact(relative)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer file.Close()

	modTime := time.Now()
	if release, err := s.store.releaseByPath(r.Context(), relative); err == nil {
		if release.ContentType != "" {
			w.Header().Set("Content-Type", release.ContentType)
		}
		if release.FileName != "" {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(release.FileName)))
		}
		if t, parseErr := time.Parse(time.RFC3339, release.PublishedAt); parseErr == nil {
			modTime = t
		}
	}
	http.ServeContent(w, r, filepath.Base(relative), modTime, file)
}

type publishArtifact struct {
	FileName    string
	ContentType string
	Payload     []byte
}

type publishPayload struct {
	Manifest  SeedPackage
	DistTags  []string
	Readme    string
	Notes     string
	Artifacts map[string]publishArtifact
}

func (s *Service) parsePublishRequest(r *http.Request) (publishPayload, func(), error) {
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(256 << 20); err != nil {
			return publishPayload{}, func() {}, err
		}
		cleanup := func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}
		rawManifest := strings.TrimSpace(r.FormValue("manifest"))
		if rawManifest == "" {
			return publishPayload{}, cleanup, errors.New("manifest is required")
		}
		var manifest packageManifestPayload
		if err := json.Unmarshal([]byte(rawManifest), &manifest); err != nil {
			return publishPayload{}, cleanup, fmt.Errorf("invalid manifest json: %w", err)
		}
		artifacts := map[string]publishArtifact{}
		for key, files := range r.MultipartForm.File {
			if !strings.HasPrefix(key, "artifact:") || len(files) == 0 {
				continue
			}
			variantID := strings.TrimSpace(strings.TrimPrefix(key, "artifact:"))
			if variantID == "" {
				continue
			}
			fileHeader := files[0]
			file, err := fileHeader.Open()
			if err != nil {
				return publishPayload{}, cleanup, err
			}
			payload, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				return publishPayload{}, cleanup, err
			}
			artifacts[variantID] = publishArtifact{
				FileName:    fileHeader.Filename,
				ContentType: fileHeader.Header.Get("Content-Type"),
				Payload:     payload,
			}
		}
		readme := strings.TrimSpace(r.FormValue("readme_text"))
		if readme == "" && len(r.MultipartForm.File["readme"]) > 0 {
			file, err := r.MultipartForm.File["readme"][0].Open()
			if err == nil {
				if payload, readErr := io.ReadAll(file); readErr == nil {
					readme = strings.TrimSpace(string(payload))
				}
				_ = file.Close()
			}
		}
		notes := strings.TrimSpace(r.FormValue("notes_text"))
		if notes == "" && len(r.MultipartForm.File["notes"]) > 0 {
			file, err := r.MultipartForm.File["notes"][0].Open()
			if err == nil {
				if payload, readErr := io.ReadAll(file); readErr == nil {
					notes = strings.TrimSpace(string(payload))
				}
				_ = file.Close()
			}
		}
		return publishPayload{
			Manifest:  manifest.toSeedPackage(),
			DistTags:  append([]string(nil), manifest.DistTags...),
			Readme:    readme,
			Notes:     notes,
			Artifacts: artifacts,
		}, cleanup, nil
	}

	var legacy PublishRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<20)).Decode(&legacy); err != nil {
		return publishPayload{}, func() {}, err
	}
	if legacy.Package == nil {
		return publishPayload{}, func() {}, errors.New("package is required")
	}
	if strings.TrimSpace(legacy.ArtifactBase64) == "" {
		return publishPayload{}, func() {}, errors.New("artifact_base64 is required")
	}
	payloadBytes, err := decodeBase64Bytes(legacy.ArtifactBase64)
	if err != nil {
		return publishPayload{}, func() {}, err
	}
	manifest := *legacy.Package
	variantID := strings.TrimSpace(legacy.Variant)
	if variantID == "" && len(manifest.Variants) == 1 {
		variantID = strings.TrimSpace(manifest.Variants[0].ID)
	}
	if variantID == "" {
		return publishPayload{}, func() {}, errors.New("variant is required")
	}
	return publishPayload{
		Manifest: manifest,
		DistTags: []string{"latest"},
		Artifacts: map[string]publishArtifact{
			variantID: {
				FileName:    legacy.FileName,
				ContentType: legacy.ContentType,
				Payload:     payloadBytes,
			},
		},
	}, func() {}, nil
}

func (s *Service) loadCatalog(r *http.Request) ([]packageBundle, error) {
	return s.store.packageBundles(r.Context(), s.baseURL(r))
}

func (s *Service) packageBundleByName(ctx context.Context, name string) (packageBundle, error) {
	record, err := s.store.findPackage(ctx, name)
	if err != nil {
		return packageBundle{}, err
	}
	if strings.TrimSpace(record.Name) == "" {
		return packageBundle{}, errors.New("package name is required")
	}
	return s.store.loadBundle(ctx, record.Name)
}

func (s *Service) baseURL(r *http.Request) string {
	if s.publicBaseURL != "" {
		return s.publicBaseURL
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		return forwarded + "://" + r.Host
	}
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func (s *Service) joinPath(r *http.Request, rel string) string {
	return joinURL(s.baseURL(r), rel)
}

func resolvePackage(bundle packageBundle, record PackageRecord, version, goos, variantID, baseURL string) (ResolvedPackage, error) {
	selected := selectVariant(record, variantID, goos)
	if selected == nil {
		return ResolvedPackage{}, fmt.Errorf("package %s has no matching variant", record.Name)
	}

	resolvedVersion := strings.TrimSpace(version)
	if resolvedVersion == "" || resolvedVersion == "latest" {
		resolvedVersion = strings.TrimSpace(record.LatestVersion)
	}
	if resolvedVersion == "" {
		resolvedVersion = selected.Version
	}
	if resolvedVersion == "" {
		return ResolvedPackage{}, fmt.Errorf("package %s has no published versions", record.Name)
	}

	release, ok := releaseForVariantAndVersion(bundle.Releases, selected.ID, resolvedVersion)
	if !ok {
		fallbackVersion := chooseString(selected.Version, record.LatestVersion)
		fallbackURL := absoluteDownloadURL(baseURL, selected.DownloadURL)
		if strings.EqualFold(strings.TrimSpace(fallbackVersion), strings.TrimSpace(resolvedVersion)) && strings.TrimSpace(fallbackURL) != "" {
			selected = cloneVariant(*selected)
			selected.Version = fallbackVersion
			selected.DownloadURL = fallbackURL
			out := ResolvedPackage{
				Name:            record.Name,
				Title:           record.Title,
				Description:     record.Description,
				Homepage:        record.Homepage,
				LatestVersion:   record.LatestVersion,
				ResolvedVersion: resolvedVersion,
				Variant:         *selected,
			}
			return out, nil
		}
		return ResolvedPackage{}, fmt.Errorf("package %s variant %s version %s is not published", record.Name, selected.ID, resolvedVersion)
	}
	selected = cloneVariant(*selected)
	selected.Version = release.Version
	selected.FileName = release.FileName
	selected.SHA256 = release.SHA256
	selected.DownloadURL = joinURL(baseURL, fileURLPath(release.RelativePath))

	out := ResolvedPackage{
		Name:            record.Name,
		Title:           record.Title,
		Description:     record.Description,
		Homepage:        record.Homepage,
		LatestVersion:   record.LatestVersion,
		ResolvedVersion: resolvedVersion,
		Variant:         *selected,
	}
	return out, nil
}

type packageManifestPayload struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Homepage    string                   `json:"homepage"`
	Aliases     []string                 `json:"aliases"`
	DistTags    []string                 `json:"dist_tags"`
	Variants    []packageManifestVariantPayload `json:"variants"`
}

type packageManifestVariantPayload struct {
	ID                string         `json:"id"`
	Label             string         `json:"label"`
	OS                string         `json:"os"`
	Default           bool           `json:"default"`
	IsDefault         bool           `json:"is_default"`
	FileName          string         `json:"file_name"`
	InstallStrategy   string         `json:"install_strategy"`
	UninstallStrategy string         `json:"uninstall_strategy"`
	InstallRoot       string         `json:"install_root"`
	BinaryName        string         `json:"binary_name"`
	WrapperName       string         `json:"wrapper_name"`
	LauncherPath      string         `json:"launcher_path"`
	Metadata          map[string]any `json:"metadata"`
}

func (payload packageManifestPayload) toSeedPackage() SeedPackage {
	result := SeedPackage{
		Name:        canonicalPackageName(payload.Name),
		Version:     strings.TrimSpace(payload.Version),
		Aliases:     append([]string(nil), payload.Aliases...),
		Title:       strings.TrimSpace(payload.Title),
		Description: strings.TrimSpace(payload.Description),
		Homepage:    strings.TrimSpace(payload.Homepage),
		DistTags:    append([]string(nil), payload.DistTags...),
		Variants:    make([]SeedVariant, 0, len(payload.Variants)),
	}
	for _, variant := range payload.Variants {
		result.Variants = append(result.Variants, SeedVariant{
			ID:                strings.TrimSpace(variant.ID),
			Label:             strings.TrimSpace(variant.Label),
			OS:                normalizeOS(variant.OS),
			Default:           variant.Default,
			IsDefault:         variant.IsDefault,
			FileName:          strings.TrimSpace(variant.FileName),
			InstallStrategy:   strings.TrimSpace(variant.InstallStrategy),
			UninstallStrategy: strings.TrimSpace(variant.UninstallStrategy),
			InstallRoot:       strings.TrimSpace(variant.InstallRoot),
			BinaryName:        strings.TrimSpace(variant.BinaryName),
			WrapperName:       strings.TrimSpace(variant.WrapperName),
			LauncherPath:      strings.TrimSpace(variant.LauncherPath),
			Metadata:          variant.Metadata,
		})
	}
	return result
}

func uniqueReleaseVersions(releases []sqliteReleaseRow) []string {
	seen := map[string]struct{}{}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		version := strings.TrimSpace(release.Version)
		if version == "" {
			continue
		}
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return compareVersionStrings(versions[i], versions[j]) > 0
	})
	return versions
}

func knownVersions(record PackageRecord, releases []sqliteReleaseRow) []string {
	versions := uniqueReleaseVersions(releases)
	if len(versions) > 0 {
		return versions
	}
	seen := map[string]struct{}{}
	appendVersion := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		versions = append(versions, value)
	}
	appendVersion(record.LatestVersion)
	for _, variant := range record.Variants {
		appendVersion(variant.Version)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return compareVersionStrings(versions[i], versions[j]) > 0
	})
	return versions
}

func packageVersionView(bundle packageBundle, record PackageRecord, version string, distTags []string, readme string, baseURL string) PackageVersionView {
	selected := strings.TrimSpace(version)
	if selected == "" || selected == "latest" {
		selected = record.LatestVersion
	}
	variants := make([]PackageVariant, 0, len(record.Variants))
	publishedAt := ""
	for _, variant := range record.Variants {
		release, ok := releaseForVariantAndVersion(bundle.Releases, variant.ID, selected)
		if !ok {
			if !strings.EqualFold(strings.TrimSpace(variant.Version), strings.TrimSpace(selected)) {
				continue
			}
			clone := cloneVariant(variant)
			clone.DownloadURL = absoluteDownloadURL(baseURL, clone.DownloadURL)
			if strings.TrimSpace(clone.DownloadURL) == "" {
				continue
			}
			variants = append(variants, clone)
			continue
		}
		clone := cloneVariant(variant)
		clone.Version = release.Version
		clone.FileName = release.FileName
		clone.SHA256 = release.SHA256
		clone.DownloadURL = joinURL(baseURL, fileURLPath(release.RelativePath))
		variants = append(variants, clone)
		if publishedAt == "" || release.PublishedAt > publishedAt {
			publishedAt = release.PublishedAt
		}
	}
	if len(distTags) == 0 {
		distTags = []string{"latest"}
	}
	return PackageVersionView{
		Version:     selected,
		Description: record.Description,
		Homepage:    record.Homepage,
		PublishedAt: publishedAt,
		Readme:      strings.TrimSpace(readme),
		Variants:    variants,
		DistTags:    append([]string(nil), distTags...),
	}
}

func chooseString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func filterPackageRecordByOS(record PackageRecord, goos string) PackageRecord {
	record.Variants = filterVariantsByOS(record.Variants, goos)
	return record
}

func filterVariantsByOS(variants []PackageVariant, goos string) []PackageVariant {
	if goos == "" || goos == "all" {
		return append([]PackageVariant(nil), variants...)
	}
	filtered := make([]PackageVariant, 0, len(variants))
	for _, variant := range variants {
		if normalizeOS(variant.OS) == goos {
			filtered = append(filtered, variant)
		}
	}
	return filtered
}

func resolveRequestedVersion(requested, latest string, distTags map[string]string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" || strings.EqualFold(requested, "latest") {
		if mapped := strings.TrimSpace(distTags["latest"]); mapped != "" {
			return mapped
		}
		return strings.TrimSpace(latest)
	}
	if mapped := strings.TrimSpace(distTags[strings.ToLower(requested)]); mapped != "" {
		return mapped
	}
	return requested
}

func tagsForVersion(distTags map[string]string, version string) []string {
	matches := make([]string, 0, len(distTags))
	for tag, taggedVersion := range distTags {
		if strings.EqualFold(strings.TrimSpace(taggedVersion), strings.TrimSpace(version)) {
			matches = append(matches, tag)
		}
	}
	sort.Strings(matches)
	return matches
}

func distTagMapFromSlice(tags []string, version string) map[string]string {
	result := map[string]string{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		result[tag] = version
	}
	if len(result) == 0 {
		result["latest"] = version
	}
	return result
}

func absoluteDownloadURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	return joinURL(baseURL, raw)
}

func (s *Service) authorized(r *http.Request) bool {
	if s.publishToken == "" {
		return true
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		return false
	}
	token := strings.TrimSpace(authorization[len("Bearer "):])
	return token == s.publishToken
}

func releaseForVariantAndVersion(releases []sqliteReleaseRow, variantID, version string) (sqliteReleaseRow, bool) {
	var (
		found bool
		best  sqliteReleaseRow
	)
	for _, release := range releases {
		if !strings.EqualFold(release.VariantID, variantID) {
			continue
		}
		if strings.TrimSpace(version) != "" && !strings.EqualFold(release.Version, version) {
			continue
		}
		if !found || compareVersionStrings(release.Version, best.Version) > 0 || (release.Version == best.Version && release.PublishedAt > best.PublishedAt) {
			best = release
			found = true
		}
	}
	return best, found
}

func cloneVariant(variant PackageVariant) PackageVariant {
	clone := variant
	if variant.Notes != nil {
		clone.Notes = append([]string(nil), variant.Notes...)
	}
	if variant.Metadata != nil {
		clone.Metadata = map[string]any{}
		for key, value := range variant.Metadata {
			clone.Metadata[key] = value
		}
	}
	if variant.Source != nil {
		clone.Source = map[string]any{}
		for key, value := range variant.Source {
			clone.Source[key] = value
		}
	}
	return clone
}

func selectVariant(record PackageRecord, variantID, goos string) *PackageVariant {
	if len(record.Variants) == 0 {
		return nil
	}
	normalizedOS := normalizeOS(goos)
	if variantID != "" {
		for i := range record.Variants {
			if strings.EqualFold(record.Variants[i].ID, variantID) {
				return &record.Variants[i]
			}
		}
		return nil
	}

	var fallback *PackageVariant
	for i := range record.Variants {
		variant := &record.Variants[i]
		if fallback == nil {
			fallback = variant
		}
		if normalizedOS != "" && normalizeOS(variant.OS) != normalizedOS {
			continue
		}
		if variant.Default || variant.IsDefault {
			return variant
		}
		if fallback == nil {
			fallback = variant
		}
	}
	if fallback != nil {
		return fallback
	}
	return &record.Variants[0]
}

func releaseFromRow(row sqliteReleaseRow, baseURL string) PackageRelease {
	publishedAt, _ := time.Parse(time.RFC3339, row.PublishedAt)
	return PackageRelease{
		PackageName:  row.PackageName,
		VariantID:    row.VariantID,
		Version:      row.Version,
		FileName:     row.FileName,
		RelativePath: row.RelativePath,
		DownloadURL:  joinURL(baseURL, fileURLPath(row.RelativePath)),
		ContentType:  row.ContentType,
		SHA256:       row.SHA256,
		Size:         row.Size,
		PublishedAt:  publishedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"error":   err.Error(),
	})
}
