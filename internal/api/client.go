package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type ManifestResponse struct {
	Success   bool               `json:"success"`
	Artifacts []ManifestArtifact `json:"artifacts"`
	Error     string             `json:"error"`
}

type ManifestArtifact struct {
	Platform       string         `json:"platform"`
	Channel        string         `json:"channel"`
	Version        string         `json:"version"`
	SHA256         string         `json:"sha256"`
	DownloadURL    string         `json:"download_url"`
	InstallCommand string         `json:"install_command"`
	FileName       string         `json:"file_name"`
	Metadata       map[string]any `json:"metadata"`
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://sosiskibot.ru/basedata"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *Client) ReleaseManifest() (*ManifestResponse, error) {
	resp, err := c.http.Get(c.baseURL + "/api/releases/manifest")
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

	var parsed ManifestResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if !parsed.Success && strings.TrimSpace(parsed.Error) != "" {
		return nil, fmt.Errorf(parsed.Error)
	}
	return &parsed, nil
}

func (m *ManifestResponse) Artifact(platform string) *ManifestArtifact {
	needle := strings.ToLower(strings.TrimSpace(platform))
	for index := range m.Artifacts {
		if strings.ToLower(strings.TrimSpace(m.Artifacts[index].Platform)) == needle {
			return &m.Artifacts[index]
		}
	}
	return nil
}
