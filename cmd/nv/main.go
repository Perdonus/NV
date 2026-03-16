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
)

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
		return uninstallPackage(client, args[1])
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

	step(1, 3, fmt.Sprintf("получаем пакет %s", name))
	resolved, err := client.ResolvePackage(name, version, runtime.GOOS, "")
	if err != nil {
		return fmt.Errorf("реестр пакетов недоступен: %w", err)
	}
	if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
		return errors.New(strings.TrimSpace(resolved.Error))
	}
	if strings.TrimSpace(resolved.Package.Name) == "" || strings.TrimSpace(resolved.Package.Variant.DownloadURL) == "" {
		return fmt.Errorf("реестр пакетов вернул неполный пакет %s", name)
	}
	return installResolvedPackage(&resolved.Package)
}

func installResolvedPackage(pkg *api.ResolvedPackage) error {
	switch pkg.Variant.InstallStrategy {
	case "linux-cli-wrapper":
		return installLinuxCLIWrapperPackage(pkg)
	case "linux-portable-tar":
		return installLinuxPortableTarPackage(pkg)
	case "windows-portable-zip":
		return installWindowsPortableZipPackage(pkg)
	case "unix-self-binary":
		return installUnixBinaryPackage(pkg)
	case "windows-self-binary":
		return installWindowsSelfBinaryPackage(pkg)
	default:
		return fmt.Errorf("пакет %s использует неподдерживаемую install strategy %q", pkg.Name, pkg.Variant.InstallStrategy)
	}
}

func installLinuxCLIWrapperPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "share", "neuralv-shell"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	binaryName := strings.TrimSpace(pkg.Variant.BinaryName)
	if binaryName == "" {
		binaryName = "neuralv-shell"
	}
	target := filepath.Join(installRoot, binaryName)
	step(2, 4, fmt.Sprintf("скачиваем %s", binaryName))
	if err := downloadArtifactBinary(pkg.Variant.DownloadURL, target, binaryName); err != nil {
		return err
	}

	hasDaemon := false
	if daemonURL, ok := metadataString(pkg.Variant.Metadata, "daemonUrl"); ok && strings.TrimSpace(daemonURL) != "" {
		hasDaemon = true
		step(3, 4, "скачиваем daemon")
		if err := downloadArtifactBinary(daemonURL, filepath.Join(installRoot, "neuralvd"), "neuralvd"); err != nil {
			return err
		}
	} else {
		step(3, 4, "daemon не опубликован, пропускаем")
	}

	wrapperName := strings.TrimSpace(pkg.Variant.WrapperName)
	if wrapperName == "" {
		wrapperName = pkg.Name
	}
	wrapperDir, err := resolveInstallRoot("$HOME/.local/bin", filepath.Join("$HOME", ".local", "bin"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		return err
	}
	step(4, 4, "обновляем launcher")
	wrapper := filepath.Join(wrapperDir, wrapperName)
	wrapperBody := fmt.Sprintf("#!/usr/bin/env sh\nexec %q \"$@\"\n", target)
	if err := writeExecutableFile(wrapper, []byte(wrapperBody)); err != nil {
		return err
	}

	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", installRoot)
	printPathHint(wrapperDir)
	if hasDaemon {
		fmt.Printf("Daemon: %s\n", filepath.Join(installRoot, "neuralvd"))
	}
	return nil
}

func installLinuxPortableTarPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "opt", pkg.Title))
	if err != nil {
		return err
	}
	parentDir := filepath.Dir(installRoot)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "nv-linux-gui-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	step(2, 4, "скачиваем архив")
	archivePath := filepath.Join(tmpDir, "package.tar.gz")
	if err := downloadRawFile(pkg.Variant.DownloadURL, archivePath); err != nil {
		return err
	}

	step(3, 4, "распаковываем пакет")
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := extractTarArchive(archivePath, extractDir); err != nil {
		return err
	}

	step(4, 4, "обновляем директорию")
	if err := replaceExtractedDirectory(extractDir, installRoot); err != nil {
		return err
	}

	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", installRoot)
	return nil
}

func installWindowsPortableZipPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, `%LOCALAPPDATA%/NeuralV`)
	if err != nil {
		return err
	}
	parentDir := filepath.Dir(installRoot)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "nv-win-package-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	step(2, 4, "скачиваем архив")
	archivePath := filepath.Join(tmpDir, "package.zip")
	if err := downloadRawFile(pkg.Variant.DownloadURL, archivePath); err != nil {
		return err
	}

	step(3, 4, "распаковываем пакет")
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := extractZip(archivePath, extractDir); err != nil {
		return err
	}

	step(4, 4, "обновляем директорию")
	if err := replaceExtractedDirectory(extractDir, installRoot); err != nil {
		return err
	}

	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", installRoot)
	return nil
}

func installUnixBinaryPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, "$HOME/.local/bin")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	binaryName := strings.TrimSpace(pkg.Variant.BinaryName)
	if binaryName == "" {
		binaryName = pkg.Name
	}
	target := filepath.Join(installRoot, binaryName)

	step(2, 3, fmt.Sprintf("скачиваем %s", binaryName))
	if err := downloadArtifactBinary(pkg.Variant.DownloadURL, target, binaryName); err != nil {
		return err
	}

	step(3, 3, "готово")
	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", target)
	printPathHint(installRoot)
	return nil
}

func installWindowsSelfBinaryPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, `%LOCALAPPDATA%/NV`)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return err
	}

	binaryName := strings.TrimSpace(pkg.Variant.BinaryName)
	if binaryName == "" {
		binaryName = pkg.Name + ".exe"
	}
	target := filepath.Join(installRoot, binaryName)
	stagePath := filepath.Join(installRoot, binaryName+".next")
	scriptPath := filepath.Join(installRoot, "nv-update.cmd")
	_ = os.Remove(stagePath)
	_ = os.Remove(scriptPath)

	step(2, 3, fmt.Sprintf("скачиваем %s", binaryName))
	if err := downloadRawFile(pkg.Variant.DownloadURL, stagePath); err != nil {
		return err
	}

	step(3, 3, "обновляем пакет")
	if runningCurrentExecutable(target) {
		if err := scheduleWindowsSelfReplace(stagePath, target, scriptPath); err != nil {
			_ = os.Remove(stagePath)
			return err
		}
		fmt.Printf("Пакет %s обновляется до версии %s\n", pkg.Name, pkg.ResolvedVersion)
		fmt.Printf("Путь: %s\n", target)
		fmt.Println("Замена будет завершена после выхода текущего процесса.")
		return nil
	}
	if err := replaceFile(stagePath, target); err != nil {
		_ = os.Remove(stagePath)
		return err
	}

	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", target)
	return nil
}

func uninstallPackage(client *api.Client, name string) error {
	normalized := normalizePackageName(name)
	if normalized == "" {
		return errors.New("empty package name")
	}

	resolved, err := client.ResolvePackage(normalized, "latest", runtime.GOOS, "")
	if err != nil {
		return fmt.Errorf("реестр пакетов недоступен: %w", err)
	}
	if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
		return errors.New(strings.TrimSpace(resolved.Error))
	}
	return uninstallResolvedPackage(&resolved.Package)
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

func resolveInstallRoot(configuredRoot, fallback string) (string, error) {
	root := strings.TrimSpace(configuredRoot)
	if root == "" {
		root = strings.TrimSpace(fallback)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	replacements := map[string]string{
		"$HOME":           home,
		"%USERPROFILE%":   home,
		"%LOCALAPPDATA%":  os.Getenv("LOCALAPPDATA"),
		"%APPDATA%":       os.Getenv("APPDATA"),
	}
	for token, value := range replacements {
		if value == "" {
			continue
		}
		root = strings.ReplaceAll(root, token, value)
	}

	return filepath.Clean(root), nil
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

func packageVersion(packageName, version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("реестр пакета %s не содержит version", packageName)
	}
	if !semverPattern.MatchString(version) {
		return "", fmt.Errorf("реестр пакета %s содержит некорректную версию %q", packageName, version)
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
	resolved, err := client.ResolvePackage("nv", "latest", runtime.GOOS, "")
	if err != nil {
		return "", err
	}
	if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
		return "", errors.New(strings.TrimSpace(resolved.Error))
	}
	return packageVersion("nv", resolved.Package.ResolvedVersion)
}

func uninstallResolvedPackage(pkg *api.ResolvedPackage) error {
	switch pkg.Variant.UninstallStrategy {
	case "windows-remove-dir":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, `%LOCALAPPDATA%/NeuralV`)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		fmt.Printf("Пакет %s удален из %s\n", pkg.Name, root)
		return nil
	case "linux-remove-dir":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "opt", pkg.Title))
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		fmt.Printf("Пакет %s удален из %s\n", pkg.Name, root)
		return nil
	case "linux-cli-wrapper-remove":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "share", "neuralv-shell"))
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		wrapperName := strings.TrimSpace(pkg.Variant.WrapperName)
		if wrapperName == "" {
			wrapperName = pkg.Name
		}
		wrapperDir, err := resolveInstallRoot("$HOME/.local/bin", filepath.Join("$HOME", ".local", "bin"))
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(wrapperDir, wrapperName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Printf("Пакет %s удален из %s\n", pkg.Name, root)
		return nil
	default:
		return fmt.Errorf("пакет %s не поддерживает uninstall", pkg.Name)
	}
}

func extractTarArchive(archivePath, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
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
		targetPath := filepath.Join(targetDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode)
			if mode == 0 {
				mode = 0o755
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceExtractedDirectory(extractDir, installRoot string) error {
	entryRoot := extractDir
	entries, err := os.ReadDir(extractDir)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		entryRoot = filepath.Join(extractDir, entries[0].Name())
	}
	if err := os.RemoveAll(installRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(installRoot), 0o755); err != nil {
		return err
	}
	return os.Rename(entryRoot, installRoot)
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
