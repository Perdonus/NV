package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://sosiskibot.ru/basedata"

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

type PackageRecord struct {
	Name          string           `json:"name"`
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
	SHA256            string         `json:"sha256"`
	InstallStrategy   string         `json:"install_strategy"`
	UninstallStrategy string         `json:"uninstall_strategy"`
	InstallRoot       string         `json:"install_root"`
	BinaryName        string         `json:"binary_name"`
	WrapperName       string         `json:"wrapper_name"`
	LauncherPath      string         `json:"launcher_path"`
	Notes             []string       `json:"notes"`
	Metadata          map[string]any `json:"metadata"`
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

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *Client) ListPackages(goos string) (*PackageCatalogResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" {
		query.Set("os", normalized)
	}
	return getJSON[PackageCatalogResponse](c, "/api/packages", query)
}

func (c *Client) PackageDetails(name, goos string) (*PackageDetailResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" {
		query.Set("os", normalized)
	}
	response, err := getJSON[PackageDetailResponse](c, fmt.Sprintf("/api/packages/%s", url.PathEscape(backendPackageName(name))), query)
	if err != nil {
		return nil, err
	}
	canonicalizePackageRecord(&response.Package)
	return response, nil
}

func (c *Client) ResolvePackage(name, version, goos, variant string) (*PackageResolveResponse, error) {
	query := url.Values{}
	if normalized := normalizeOS(goos); normalized != "" {
		query.Set("os", normalized)
	}
	if strings.TrimSpace(version) != "" {
		query.Set("version", strings.TrimSpace(version))
	}
	if strings.TrimSpace(variant) != "" {
		query.Set("variant", strings.TrimSpace(variant))
	}
	response, err := getJSON[PackageResolveResponse](c, fmt.Sprintf("/api/packages/%s/resolve", url.PathEscape(backendPackageName(name))), query)
	if err != nil {
		return nil, err
	}
	canonicalizeResolvedPackage(&response.Package)
	return response, nil
}

func getJSON[T any](c *Client, route string, query url.Values) (*T, error) {
	fullURL := c.baseURL + route
	if query != nil && len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	resp, err := c.http.Get(fullURL)
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
	}
	return &parsed, nil
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

func backendPackageName(name string) string {
	switch canonicalPackageName(name) {
	case "@lvls/neuralv":
		return "neuralv"
	case "@lvls/nv":
		return "nv"
	default:
		return strings.TrimSpace(name)
	}
}

func canonicalPackageName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "neuralv", "@lvls/neuralv":
		return "@lvls/neuralv"
	case "nv", "@lvls/nv":
		return "@lvls/nv"
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
