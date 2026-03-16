package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Perdonus/NV/internal/api"
)

const defaultBaseURL = "https://sosiskibot.ru/basedata"

var (
	nvVersion     = "dev"
	semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	packageRegistry = map[string]packageDefinition{
		"neuralv": {
			install:   installNeuralVPackage,
			uninstall: uninstallNeuralVPackage,
		},
		"nv": {
			install: installNVPackage,
		},
	}
)

type packageDefinition struct {
	install   func(client *api.Client, version string) error
	uninstall func() error
}

type semver struct {
	major int
	minor int
	patch int
}

func main() {
	client := api.NewClient(resolveBaseURL())
	if err := handle(os.Args[1:], client); err != nil {
		fmt.Fprintln(os.Stderr, "nv error:", err)
		os.Exit(1)
	}
}

func resolveBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("NEURALV_BASE_URL"))
	if baseURL == "" {
		return defaultBaseURL
	}
	return baseURL
}

func handle(args []string, client *api.Client) error {
	warnIfNVUpdateAvailable(args, client)

	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "-v", "--version", "version":
		fmt.Printf("nv %s\n", nvVersion)
		return nil
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "install":
		if len(args) < 2 {
			return errors.New("не хватает спецификации пакета: install <package[@version]>")
		}
		return installPackage(client, args[1])
	case "uninstall":
		if len(args) < 2 {
			return errors.New("не хватает имени пакета: uninstall <package>")
		}
		return uninstallPackage(args[1])
	default:
		printHelp()
		return fmt.Errorf("неизвестная команда: %s", args[0])
	}
}

func printHelp() {
	fmt.Println(`nv

Команды:
  install <package[@version]>
  uninstall <package>
  version | -v | --version
  help | -h | --help

Аргументы:
  <package>
  <version>: latest | <major.minor.patch>`)
}

func installPackage(client *api.Client, spec string) error {
	name, version, err := parsePackageSpec(spec)
	if err != nil {
		return err
	}

	definition, ok := packageRegistry[name]
	if !ok {
		return fmt.Errorf("неизвестный пакет: %s", name)
	}
	if definition.install == nil {
		return fmt.Errorf("пакет %s не поддерживает install", name)
	}
	return definition.install(client, version)
}

func installNeuralVPackage(client *api.Client, requestedVersion string) error {
	step(1, 4, "читаем manifest пакета neuralv")
	manifest, err := client.ReleaseManifest()
	if err != nil {
		return fmt.Errorf("manifest пакета neuralv недоступен: %w", err)
	}

	artifact, err := neuralVArtifactForCurrentPlatform(manifest)
	if err != nil {
		return err
	}

	version, err := matchRequestedVersion("neuralv", requestedVersion, artifact)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		return installNeuralVWindowsPackage(artifact, version)
	}
	return installNeuralVUnixPackage(artifact, version)
}

func installNVPackage(client *api.Client, requestedVersion string) error {
	step(1, 3, "читаем manifest пакета nv")
	manifest, err := client.NVManifest(runtime.GOOS)
	if err != nil {
		return fmt.Errorf("manifest пакета nv недоступен: %w", err)
	}

	artifact, err := nvArtifactForCurrentPlatform(manifest)
	if err != nil {
		return err
	}

	version, err := matchRequestedVersion("nv", requestedVersion, artifact)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		return installNVWindowsPackage(artifact, version)
	}
	return installNVUnixPackage(artifact, version)
}

func installNeuralVUnixPackage(artifact *api.ManifestArtifact, version string) error {
	installRoot, err := defaultLinuxInstallRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	shellTarget := filepath.Join(installRoot, "neuralv-shell")
	step(2, 4, "скачиваем neuralv-shell")
	if err := downloadArtifactBinary(artifact.DownloadURL, shellTarget, "neuralv-shell"); err != nil {
		return err
	}

	hasDaemon := false
	if daemonURL, ok := metadataString(artifact.Metadata, "daemonUrl"); ok && strings.TrimSpace(daemonURL) != "" {
		hasDaemon = true
		step(3, 4, "скачиваем neuralvd")
		daemonTarget := filepath.Join(installRoot, "neuralvd")
		if err := downloadArtifactBinary(daemonURL, daemonTarget, "neuralvd"); err != nil {
			return err
		}
	} else {
		step(3, 4, "daemon пока не опубликован, пропускаем")
	}

	step(4, 4, "обновляем launcher")
	wrapper := filepath.Join(installRoot, "neuralv")
	wrapperBody := fmt.Sprintf("#!/usr/bin/env sh\nexec %q \"$@\"\n", shellTarget)
	if err := writeExecutableFile(wrapper, []byte(wrapperBody)); err != nil {
		return err
	}

	fmt.Printf("\nПакет neuralv установлен или обновлен до версии %s\n", version)
	fmt.Printf("Путь: %s\n", installRoot)
	if hasDaemon {
		fmt.Printf("Daemon: %s\n", filepath.Join(installRoot, "neuralvd"))
	}
	printPathHint(installRoot)
	return nil
}

func installNeuralVWindowsPackage(artifact *api.ManifestArtifact, version string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	installRoot := filepath.Join(home, "AppData", "Local", "NeuralV")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "nv-win-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	step(2, 4, "скачиваем windows bundle")
	archivePath := filepath.Join(tmpDir, "neuralv-windows.zip")
	if err := downloadRawFile(artifact.DownloadURL, archivePath); err != nil {
		return err
	}

	step(3, 4, "распаковываем bundle")
	if err := extractZip(archivePath, installRoot); err != nil {
		return err
	}

	step(4, 4, "готово")
	fmt.Printf("Пакет neuralv установлен или обновлен до версии %s\n", version)
	fmt.Printf("Путь: %s\n", installRoot)
	return nil
}

func installNVUnixPackage(artifact *api.ManifestArtifact, version string) error {
	installRoot, err := defaultLinuxInstallRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	target := filepath.Join(installRoot, "nv")
	step(2, 3, "скачиваем и обновляем nv")
	if err := downloadArtifactBinary(artifact.DownloadURL, target, "nv"); err != nil {
		return err
	}

	step(3, 3, "готово")
	fmt.Printf("Пакет nv установлен или обновлен до версии %s\n", version)
	fmt.Printf("Путь: %s\n", target)
	printPathHint(installRoot)
	return nil
}

func installNVWindowsPackage(artifact *api.ManifestArtifact, version string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	installRoot := filepath.Join(home, "AppData", "Local", "NV")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	target := filepath.Join(installRoot, "nv.exe")
	stagePath := filepath.Join(installRoot, "nv.next.exe")
	scriptPath := filepath.Join(installRoot, "nv-update.cmd")
	_ = os.Remove(stagePath)
	_ = os.Remove(scriptPath)

	step(2, 3, "скачиваем nv")
	if err := downloadRawFile(artifact.DownloadURL, stagePath); err != nil {
		return err
	}

	step(3, 3, "обновляем nv")
	if runningCurrentExecutable(target) {
		if err := scheduleWindowsSelfReplace(stagePath, target, scriptPath); err != nil {
			_ = os.Remove(stagePath)
			return err
		}
		fmt.Printf("Пакет nv обновляется до версии %s\n", version)
		fmt.Printf("Путь: %s\n", target)
		fmt.Println("Замена будет завершена после выхода текущего процесса.")
		return nil
	}

	if err := replaceFile(stagePath, target); err != nil {
		_ = os.Remove(stagePath)
		return err
	}

	fmt.Printf("Пакет nv установлен или обновлен до версии %s\n", version)
	fmt.Printf("Путь: %s\n", target)
	return nil
}

func uninstallPackage(name string) error {
	normalized := normalizePackageName(name)
	if normalized == "" {
		return errors.New("empty package name")
	}

	definition, ok := packageRegistry[normalized]
	if !ok {
		return fmt.Errorf("неизвестный пакет: %s", normalized)
	}
	if definition.uninstall == nil {
		return fmt.Errorf("пакет %s не поддерживает uninstall", normalized)
	}
	return definition.uninstall()
}

func uninstallNeuralVPackage() error {
	switch runtime.GOOS {
	case "windows":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		root := filepath.Join(home, "AppData", "Local", "NeuralV")
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		fmt.Printf("Пакет neuralv удален из %s\n", root)
	default:
		installRoot, err := defaultLinuxInstallRoot()
		if err != nil {
			return err
		}
		for _, target := range []string{"neuralv", "neuralv-shell", "neuralvd"} {
			path := filepath.Join(installRoot, target)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		fmt.Printf("Пакет neuralv удален из %s\n", installRoot)
	}

	return nil
}

func parsePackageSpec(spec string) (string, string, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return "", "", errors.New("empty package spec")
	}

	parts := strings.SplitN(raw, "@", 2)
	name := normalizePackageName(parts[0])
	if name == "" {
		return "", "", errors.New("empty package name")
	}

	version := "latest"
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		version = strings.TrimSpace(parts[1])
	}
	if version != "latest" && !semverPattern.MatchString(version) {
		return "", "", fmt.Errorf("некорректная версия %q: используй latest или semver 1.2.3", version)
	}
	return name, version, nil
}

func normalizePackageName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func defaultLinuxInstallRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func metadataString(metadata map[string]any, key string) (string, bool) {
	if metadata == nil {
		return "", false
	}
	value, ok := metadata[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func downloadRawFile(url, target string) error {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("ошибка скачивания артефакта: http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return writeRegularFile(target, response.Body, 0o755)
}

func downloadArtifactBinary(url, target, expectedName string) error {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("ошибка скачивания артефакта: http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	lowerURL := strings.ToLower(url)
	if strings.HasSuffix(lowerURL, ".tar.gz") || strings.HasSuffix(lowerURL, ".tgz") {
		return extractTarball(response.Body, target, expectedName)
	}
	return copyToTarget(response.Body, target)
}

func writeRegularFile(target string, reader io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, reader)
	return err
}

func writeExecutableFile(target string, data []byte) error {
	return copyToTarget(strings.NewReader(string(data)), target)
}

func copyToTarget(reader io.Reader, target string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dir, ".nv-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o755); err != nil {
		tempFile.Close()
		return err
	}
	if _, err := io.Copy(tempFile, reader); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	return replaceFile(tempPath, target)
}

func replaceFile(source, target string) error {
	if runtime.GOOS == "windows" {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(source, target)
}

func extractTarball(reader io.Reader, target, expectedName string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Base(header.Name)
		if name != expectedName {
			continue
		}
		return copyToTarget(tarReader, target)
	}
	return fmt.Errorf("%s не найден внутри скачанного архива", expectedName)
}

func extractZip(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		targetPath := filepath.Join(targetDir, file.Name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o755
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			src.Close()
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			return err
		}
		dst.Close()
		src.Close()
	}
	return nil
}

func step(index, total int, text string) {
	fmt.Printf("[%d/%d] %s\n", index, total, text)
}

func printPathHint(installRoot string) {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return
	}
	for _, item := range filepath.SplitList(pathEnv) {
		if sameFilePath(item, installRoot) {
			return
		}
	}
	fmt.Printf("PATH: добавь %s в PATH, если пакет не находится сразу.\n", installRoot)
}

func neuralVArtifactForCurrentPlatform(manifest *api.ManifestResponse) (*api.ManifestArtifact, error) {
	platform := "shell"
	missingArtifactErr := "linux shell-артефакт neuralv пока не опубликован"
	if runtime.GOOS == "windows" {
		platform = "windows"
		missingArtifactErr = "windows-артефакт neuralv пока не опубликован"
	}

	artifact := manifest.Artifact(platform)
	if artifact == nil || strings.TrimSpace(artifact.DownloadURL) == "" {
		return nil, errors.New(missingArtifactErr)
	}
	return artifact, nil
}

func nvArtifactForCurrentPlatform(manifest *api.ManifestResponse) (*api.ManifestArtifact, error) {
	platform, err := nvPlatform(runtime.GOOS)
	if err != nil {
		return nil, err
	}

	artifact := manifest.Artifact(platform)
	if artifact == nil || strings.TrimSpace(artifact.DownloadURL) == "" {
		return nil, fmt.Errorf("артефакт %s пока не опубликован", platform)
	}
	return artifact, nil
}

func nvPlatform(goos string) (string, error) {
	switch goos {
	case "linux":
		return "nv-linux", nil
	case "windows":
		return "nv-windows", nil
	default:
		return "", fmt.Errorf("пакет nv не поддерживает платформу %s", goos)
	}
}

func matchRequestedVersion(packageName, requestedVersion string, artifact *api.ManifestArtifact) (string, error) {
	publishedVersion, err := artifactVersion(packageName, artifact)
	if err != nil {
		return "", err
	}
	if requestedVersion != "latest" && publishedVersion != requestedVersion {
		return "", fmt.Errorf("запрошен %s@%s, но опубликована версия %s", packageName, requestedVersion, publishedVersion)
	}
	return publishedVersion, nil
}

func artifactVersion(packageName string, artifact *api.ManifestArtifact) (string, error) {
	version := strings.TrimSpace(artifact.Version)
	if version == "" {
		return "", fmt.Errorf("manifest пакета %s не содержит version", packageName)
	}
	if !semverPattern.MatchString(version) {
		return "", fmt.Errorf("manifest пакета %s содержит некорректную версию %q", packageName, version)
	}
	return version, nil
}

func warnIfNVUpdateAvailable(args []string, client *api.Client) {
	if shouldSkipNVUpdateCheck(args) {
		return
	}
	if !semverPattern.MatchString(strings.TrimSpace(nvVersion)) {
		return
	}

	latestVersion, err := latestNVVersion(client)
	if err != nil {
		return
	}
	if compareSemver(latestVersion, nvVersion) <= 0 {
		return
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "!!! ДОСТУПЕН НОВЫЙ NV %s (сейчас %s)\n", latestVersion, nvVersion)
	fmt.Fprintln(os.Stderr, "!!! Обновление: nv install nv@latest")
	fmt.Fprintln(os.Stderr)
}

func shouldSkipNVUpdateCheck(args []string) bool {
	if len(args) < 2 || args[0] != "install" {
		return false
	}
	name, _, err := parsePackageSpec(args[1])
	if err != nil {
		return false
	}
	return name == "nv"
}

func latestNVVersion(client *api.Client) (string, error) {
	manifest, err := client.NVManifest(runtime.GOOS)
	if err != nil {
		return "", err
	}
	artifact, err := nvArtifactForCurrentPlatform(manifest)
	if err != nil {
		return "", err
	}
	return artifactVersion("nv", artifact)
}

func parseSemver(raw string) (semver, error) {
	if !semverPattern.MatchString(raw) {
		return semver{}, fmt.Errorf("invalid semver %q", raw)
	}
	parts := strings.Split(raw, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, err
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, err
	}
	return semver{major: major, minor: minor, patch: patch}, nil
}

func compareSemver(left, right string) int {
	leftVersion, err := parseSemver(left)
	if err != nil {
		return 0
	}
	rightVersion, err := parseSemver(right)
	if err != nil {
		return 0
	}

	switch {
	case leftVersion.major != rightVersion.major:
		return compareInts(leftVersion.major, rightVersion.major)
	case leftVersion.minor != rightVersion.minor:
		return compareInts(leftVersion.minor, rightVersion.minor)
	default:
		return compareInts(leftVersion.patch, rightVersion.patch)
	}
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func runningCurrentExecutable(target string) bool {
	currentExecutable, err := os.Executable()
	if err != nil {
		return false
	}
	return sameFilePath(currentExecutable, target)
}

func sameFilePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func scheduleWindowsSelfReplace(stagePath, targetPath, scriptPath string) error {
	scriptBody := fmt.Sprintf("@echo off\r\nsetlocal\r\nset \"SOURCE=%s\"\r\nset \"TARGET=%s\"\r\n:retry\r\ndel /F /Q \"%%TARGET%%\" >nul 2>&1\r\nmove /Y \"%%SOURCE%%\" \"%%TARGET%%\" >nul 2>&1\r\nif errorlevel 1 (\r\n  timeout /t 1 /nobreak >nul\r\n  goto retry\r\n)\r\ndel /F /Q \"%%~f0\" >nul 2>&1\r\n", stagePath, targetPath)
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o644); err != nil {
		return err
	}

	command := exec.Command("cmd", "/c", scriptPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Start()
}
