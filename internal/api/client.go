package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultBaseURL = "https://sosiskibot.ru/nv/api"

type Client struct {
	baseURL string
	http    *http.Client
}

type PackageCatalogResponse struct {
	Success  bool            `json:"success"`
	Packages []PackageRecord `json:"packages"`
	Error    string          `json:"error"`
}

type PackageDetailResponse struct {
	Success bool          `json:"success"`
	Package PackageRecord `json:"package"`
	Error   string        `json:"error"`
}

type PackageResolveResponse struct {
	Success bool            `json:"success"`
	Package ResolvedPackage `json:"package"`
	Error   string          `json:"error"`
}

type PackageViewResponse struct {
	Success bool        `json:"success"`
	Package PackageView `json:"package"`
	Error   string      `json:"error"`
}

type WhoAmIResponse struct {
	Success bool   `json:"success"`
	Identity string `json:"identity"`
	Error   string `json:"error"`
}

type PublishResponse struct {
	Success bool               `json:"success"`
	Package PublishedVersion   `json:"package"`
	Error   string             `json:"error"`
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
	Channel           string         `json:"channel"`
	FileName          string         `json:"file_name"`
	DownloadURL       string         `json:"download_url"`
	InstallCommand    string         `json:"install_command"`
	UpdateCommand     string         `json:"update_command"`
	SHA256            string         `json:"sha256"`
	InstallStrategy   string         `json:"install_strategy"`
	UninstallStrategy string         `json:"uninstall_strategy"`
	InstallRoot       string         `json:"install_root"`
	BinaryName        string         `json:"binary_name"`
	WrapperName       string         `json:"wrapper_name"`
	LauncherPath      string         `json:"launcher_path"`
	Notes             []string       `json:"notes"`
	Metadata          map[string]any `json:"metadata"`
	Source            map[string]any `json:"source,omitempty"`
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

type PackageView struct {
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases,omitempty"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Homepage      string            `json:"homepage"`
	LatestVersion string            `json:"latest_version"`
	DistTags      map[string]string `json:"dist_tags"`
	Versions      []string          `json:"versions"`
	Version       PackageVersion    `json:"version"`
	Variants      []PackageVariant  `json:"variants,omitempty"`
	Releases      []PackageRelease  `json:"releases,omitempty"`
}

type PackageVersion struct {
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Homepage    string           `json:"homepage"`
	PublishedAt string           `json:"published_at"`
	Readme      string           `json:"readme"`
	Variants    []PackageVariant `json:"variants"`
	DistTags    []string         `json:"dist_tags"`
}

type PackageRelease struct {
	PackageName  string `json:"package_name"`
	VariantID    string `json:"variant_id"`
	Version      string `json:"version"`
	FileName     string `json:"file_name"`
	RelativePath string `json:"relative_path"`
	DownloadURL  string `json:"download_url"`
	ContentType  string `json:"content_type,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
}

type PublishedVersion struct {
	Name          string           `json:"name"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Homepage      string           `json:"homepage"`
	LatestVersion string           `json:"latest_version"`
	DistTags      map[string]string `json:"dist_tags"`
	Version       PackageVersion   `json:"version"`
}

type PublishRequest struct {
	Manifest      []byte
	ReadmePath    string
	ArtifactPaths map[string]string
	NotesPath     string
	DryRun        bool
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) ListPackages(goos string) (*PackageCatalogResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" && normalized != "all" {
		query.Set("os", normalized)
	}
	return getJSON[PackageCatalogResponse](c, "/packages", query, "")
}

func (c *Client) PackageDetails(name, goos string) (*PackageDetailResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" && normalized != "all" {
		query.Set("os", normalized)
	}
	query.Set("name", canonicalPackageName(name))
	response, err := getJSON[PackageDetailResponse](c, "/packages/details", query, "")
	if err != nil {
		return nil, err
	}
	canonicalizePackageRecord(&response.Package)
	return response, nil
}

func (c *Client) ResolvePackage(name, version, goos, variant string) (*PackageResolveResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" && normalized != "all" {
		query.Set("os", normalized)
	}
	query.Set("name", canonicalPackageName(name))
	if strings.TrimSpace(version) != "" {
		query.Set("version", strings.TrimSpace(version))
	}
	if strings.TrimSpace(variant) != "" {
		query.Set("variant", strings.TrimSpace(variant))
	}
	response, err := getJSON[PackageResolveResponse](c, "/packages/resolve", query, "")
	if err != nil {
		return nil, err
	}
	canonicalizeResolvedPackage(&response.Package)
	return response, nil
}

func (c *Client) ViewPackage(name, version, goos string) (*PackageViewResponse, error) {
	query := url.Values{}
	query.Set("name", canonicalPackageName(name))
	if strings.TrimSpace(version) != "" {
		query.Set("version", strings.TrimSpace(version))
	}
	if normalized := normalizeOS(goos); normalized != "" && normalized != "all" {
		query.Set("os", normalized)
	}
	response, err := getJSON[PackageViewResponse](c, "/packages/view", query, "")
	if err != nil {
		return nil, err
	}
	response.Package.Name = canonicalPackageName(response.Package.Name)
	response.Package.LatestVersion = strings.TrimSpace(response.Package.LatestVersion)
	if response.Package.DistTags == nil {
		response.Package.DistTags = map[string]string{}
	}
	return response, nil
}

func (c *Client) WhoAmI(token string) (*WhoAmIResponse, error) {
	return getJSON[WhoAmIResponse](c, "/whoami", nil, strings.TrimSpace(token))
}

func (c *Client) PublishPackage(request PublishRequest, token string) (*PublishResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	manifestWriter, err := writer.CreateFormField("manifest")
	if err != nil {
		return nil, err
	}
	if _, err := manifestWriter.Write(request.Manifest); err != nil {
		return nil, err
	}

	if readmePath := strings.TrimSpace(request.ReadmePath); readmePath != "" {
		if err := addMultipartFile(writer, "readme", readmePath); err != nil {
			return nil, err
		}
	}
	if notesPath := strings.TrimSpace(request.NotesPath); notesPath != "" {
		if err := addMultipartFile(writer, "notes", notesPath); err != nil {
			return nil, err
		}
	}

	artifactIDs := make([]string, 0, len(request.ArtifactPaths))
	for artifactID := range request.ArtifactPaths {
		artifactIDs = append(artifactIDs, artifactID)
	}
	sort.Strings(artifactIDs)
	for _, artifactID := range artifactIDs {
		artifactPath := request.ArtifactPaths[artifactID]
		if strings.TrimSpace(artifactPath) == "" {
			continue
		}
		fieldName := fmt.Sprintf("artifact:%s", artifactID)
		if err := addMultipartFile(writer, fieldName, artifactPath); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	query := url.Values{}
	if request.DryRun {
		query.Set("dry_run", "true")
	}
	requestURL := c.baseURL + "/publish"
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	httpRequest, err := http.NewRequest(http.MethodPost, requestURL, &body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	if token = strings.TrimSpace(token); token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var parsed PublishResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func addMultipartFile(writer *multipart.Writer, fieldName, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}

func getJSON[T any](c *Client, route string, query url.Values, token string) (*T, error) {
	fullURL := c.baseURL + route
	if query != nil && len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	request, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed T
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	switch typed := any(&parsed).(type) {
	case *PackageCatalogResponse:
		for index := range typed.Packages {
			canonicalizePackageRecord(&typed.Packages[index])
		}
	case *PackageViewResponse:
		typed.Package.Name = canonicalPackageName(typed.Package.Name)
	}
	return &parsed, nil
}

func normalizeOS(goos string) string {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "", "all":
		return "all"
	case "windows", "linux":
		return strings.ToLower(strings.TrimSpace(goos))
	case "win32":
		return "windows"
	default:
		return strings.ToLower(strings.TrimSpace(goos))
	}
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

func canonicalizePackageRecord(record *PackageRecord) {
	if record == nil {
		return
	}
	record.Name = canonicalPackageName(record.Name)
}

func canonicalizeResolvedPackage(pkg *ResolvedPackage) {
	if pkg == nil {
		return
	}
	pkg.Name = canonicalPackageName(pkg.Name)
}
