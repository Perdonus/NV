package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageDetailsFromSeed(t *testing.T) {
	svc := mustTestService(t)

	req := httptest.NewRequest(http.MethodGet, "/packages/details?name=nv", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var response struct {
		Success bool `json:"success"`
		Package PackageRecord `json:"package"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success")
	}
	if response.Package.Name != "@lvls/nv" {
		t.Fatalf("unexpected package name: %s", response.Package.Name)
	}
	if len(response.Package.Variants) == 0 {
		t.Fatalf("expected variants")
	}
}

func TestPackagesList(t *testing.T) {
	svc := mustTestService(t)

	req := httptest.NewRequest(http.MethodGet, "/packages", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var response struct {
		Success  bool           `json:"success"`
		Packages []PackageRecord `json:"packages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success || len(response.Packages) == 0 {
		t.Fatalf("unexpected list response: %+v", response)
	}
}

func TestPublishAndResolve(t *testing.T) {
	svc := mustTestService(t)

	payload := PublishRequest{
		Name:        "@lvls/nv",
		Version:     "1.2.3",
		Variant:     "nv-linux",
		FileName:    "nv-linux.tar.gz",
		ContentType: "application/gzip",
		ArtifactBase64: "aGVsbG8=",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var publishResponse struct {
		Success bool `json:"success"`
		Package struct {
			Name    string             `json:"name"`
			DistTags map[string]string `json:"dist_tags"`
			Version PackageVersionView `json:"version"`
		} `json:"package"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &publishResponse); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if !publishResponse.Success {
		t.Fatalf("expected success")
	}
	if len(publishResponse.Package.Version.Variants) == 0 || publishResponse.Package.Version.Variants[0].DownloadURL == "" {
		t.Fatalf("expected version download url")
	}

	resolveReq := httptest.NewRequest(http.MethodGet, "/packages/resolve?name=nv&version=1.2.3&os=linux&variant=nv-linux", nil)
	resolveRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(resolveRR, resolveReq)

	if resolveRR.Code != http.StatusOK {
		t.Fatalf("unexpected resolve status: %d body=%s", resolveRR.Code, resolveRR.Body.String())
	}

	var resolveResponse struct {
		Success bool            `json:"success"`
		Package ResolvedPackage `json:"package"`
	}
	if err := json.Unmarshal(resolveRR.Body.Bytes(), &resolveResponse); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if !resolveResponse.Success {
		t.Fatalf("expected resolve success")
	}
	if resolveResponse.Package.ResolvedVersion != "1.2.3" {
		t.Fatalf("unexpected resolved version: %s", resolveResponse.Package.ResolvedVersion)
	}
	if !strings.Contains(resolveResponse.Package.Variant.DownloadURL, "/files/") {
		t.Fatalf("unexpected download url: %s", resolveResponse.Package.Variant.DownloadURL)
	}

	fileReq := httptest.NewRequest(http.MethodGet, publishResponse.Package.Version.Variants[0].DownloadURL, nil)
	fileRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(fileRR, fileReq)
	if fileRR.Code != http.StatusOK {
		t.Fatalf("unexpected file status: %d body=%s", fileRR.Code, fileRR.Body.String())
	}
	if got := fileRR.Body.String(); got != "hello" {
		t.Fatalf("unexpected file contents: %q", got)
	}
}

func TestWhoami(t *testing.T) {
	svc := mustTestService(t)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var response struct {
		Success  bool   `json:"success"`
		Identity string `json:"identity"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode whoami response: %v", err)
	}
	if !response.Success || response.Identity != "publisher" {
		t.Fatalf("unexpected whoami response: %+v", response)
	}
}

func mustTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(Config{
		DataDir:  t.TempDir(),
		SeedPath: filepath.Join("..", "..", "registry", "packages.json"),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}
