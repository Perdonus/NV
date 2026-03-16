package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Perdonus/NV/internal/api"
	"github.com/Perdonus/NV/internal/semver"
	"github.com/Perdonus/NV/internal/state"
)

const defaultBaseURL = "https://sosiskibot.ru/basedata"

var nvVersion = "dev"

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
	case "list":
		return listInstalledPackages()
	case "search":
		query := strings.Join(args[1:], " ")
		return searchPackages(client, query)
	case "info":
		if len(args) < 2 {
			return errors.New("не хватает имени пакета: info <package>")
		}
		return showPackageInfo(client, args[1])
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
	fmt.Println(`Команды:
  install <package[@version]>
  uninstall <package>
  list
  search [query]
  info <package>
  version | -v | --version
  help | -h | --help`)
}

func installPackage(client *api.Client, spec string) error {
	name, version, err := parsePackageSpec(spec)
	if err != nil {
		return err
	}

	installedState, err := state.Load()
	if err != nil {
		return fmt.Errorf("не удалось открыть локальное состояние пакетов: %w", err)
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
	resolvedVersion, err := normalizePackageVersion(name, resolved.Package.ResolvedVersion)
	if err != nil {
		return err
	}
	resolved.Package.ResolvedVersion = resolvedVersion
	if normalizedLatestVersion, err := normalizePackageVersion(name, resolved.Package.LatestVersion); err == nil {
		resolved.Package.LatestVersion = normalizedLatestVersion
	}
	if installed, ok := installedState.Get(name); ok {
		if sameInstalledPackage(installed.Package, resolved.Package) {
			fmt.Printf("Пакет %s уже установлен: %s\n", resolved.Package.Name, resolved.Package.ResolvedVersion)
			return nil
		}
		printInstallTransition(installed.Package, resolved.Package)
	}

	if err := installResolvedPackage(&resolved.Package); err != nil {
		return err
	}

	installedState.Put(resolved.Package)
	if err := state.Save(installedState); err != nil {
		return fmt.Errorf("пакет установлен, но локальное состояние не обновлено: %w", err)
	}
	return nil
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
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "share", pkg.Name))
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
	step(2, 4, fmt.Sprintf("скачиваем %s", binaryName))
	if err := downloadArtifactBinary(pkg.Variant.DownloadURL, target, binaryName); err != nil {
		return err
	}

	hasDaemon := false
	daemonBinaryName := ""
	if daemonURL, ok := metadataString(pkg.Variant.Metadata, "daemonUrl"); ok && strings.TrimSpace(daemonURL) != "" {
		hasDaemon = true
		daemonBinaryName = daemonBinaryNameForPackage(pkg, daemonURL)
		step(3, 4, "скачиваем daemon")
		if err := downloadArtifactBinary(daemonURL, filepath.Join(installRoot, daemonBinaryName), daemonBinaryName); err != nil {
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
		fmt.Printf("Daemon: %s\n", filepath.Join(installRoot, daemonBinaryName))
	}
	return nil
}

func installLinuxPortableTarPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "opt", pkg.Name))
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

	step(2, 5, "скачиваем архив")
	archivePath := filepath.Join(tmpDir, "package.tar.gz")
	if err := downloadRawFile(pkg.Variant.DownloadURL, archivePath); err != nil {
		return err
	}

	step(3, 5, "распаковываем пакет")
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := extractTarArchive(archivePath, extractDir); err != nil {
		return err
	}

	step(4, 5, "обновляем директорию")
	if err := replaceExtractedDirectory(extractDir, installRoot); err != nil {
		return err
	}

	step(5, 5, "обновляем ярлыки")
	if err := ensureLinuxDesktopIntegration(pkg, installRoot); err != nil {
		return err
	}

	fmt.Printf("Пакет %s установлен или обновлен до версии %s\n", pkg.Name, pkg.ResolvedVersion)
	fmt.Printf("Путь: %s\n", installRoot)
	return nil
}

func installWindowsPortableZipPackage(pkg *api.ResolvedPackage) error {
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join(`%LOCALAPPDATA%`, pkg.Name))
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

	step(2, 5, "скачиваем архив")
	archivePath := filepath.Join(tmpDir, "package.zip")
	if err := downloadRawFile(pkg.Variant.DownloadURL, archivePath); err != nil {
		return err
	}

	step(3, 5, "распаковываем пакет")
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := extractZip(archivePath, extractDir); err != nil {
		return err
	}

	step(4, 5, "обновляем директорию")
	if err := replaceExtractedDirectory(extractDir, installRoot); err != nil {
		return err
	}

	step(5, 5, "обновляем ярлыки")
	if err := ensureWindowsShortcuts(pkg, installRoot); err != nil {
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
	installRoot, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join(`%LOCALAPPDATA%`, pkg.Name))
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
	if err := ensureWindowsCmdWrapper(installRoot, binaryName); err != nil {
		return err
	}
	if err := ensureWindowsUserPath(installRoot); err != nil {
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

	installedState, err := state.Load()
	if err != nil {
		return fmt.Errorf("не удалось открыть локальное состояние пакетов: %w", err)
	}

	var pkg *api.ResolvedPackage
	if installed, ok := installedState.Get(normalized); ok {
		pkg = &installed.Package
	} else {
		resolved, err := client.ResolvePackage(normalized, "latest", runtime.GOOS, "")
		if err != nil {
			return fmt.Errorf("реестр пакетов недоступен: %w", err)
		}
		if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
			return errors.New(strings.TrimSpace(resolved.Error))
		}
		pkg = &resolved.Package
	}

	if err := uninstallResolvedPackage(pkg); err != nil {
		return err
	}
	installedState.Delete(normalized)
	if err := state.Save(installedState); err != nil {
		return fmt.Errorf("пакет удален, но локальное состояние не обновлено: %w", err)
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
	if version != "latest" {
		normalizedVersion, err := semver.Normalize(version)
		if err != nil {
			return "", "", fmt.Errorf("некорректная версия %q: используй latest или semver 2.0.0", version)
		}
		version = normalizedVersion
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
		"$HOME":         home,
		"%USERPROFILE%": home,
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	replacements["%LOCALAPPDATA%"] = localAppData

	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	replacements["%APPDATA%"] = appData

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

func resolveLauncherPath(pkg *api.ResolvedPackage, installRoot string, fallbacks ...string) string {
	candidates := make([]string, 0, 2+len(fallbacks))
	if launcher := strings.TrimSpace(pkg.Variant.LauncherPath); launcher != "" {
		candidates = append(candidates, launcher)
	}
	if binaryName := strings.TrimSpace(pkg.Variant.BinaryName); binaryName != "" {
		candidates = append(candidates, binaryName)
	}
	candidates = append(candidates, fallbacks...)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		absolute := filepath.Join(installRoot, filepath.FromSlash(candidate))
		if _, err := os.Stat(absolute); err == nil {
			return absolute
		}
	}
	return filepath.Join(installRoot, filepath.Base(strings.TrimSpace(pkg.Variant.LauncherPath)))
}

func ensureLinuxDesktopIntegration(pkg *api.ResolvedPackage, installRoot string) error {
	launcher := resolveLauncherPath(pkg, installRoot, "bin/NeuralV", "NeuralV", pkg.Name)
	if _, err := os.Stat(launcher); err != nil {
		return fmt.Errorf("launcher не найден после установки: %s", launcher)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	applicationsDir := filepath.Join(home, ".local", "share", "applications")
	desktopDir := filepath.Join(home, "Desktop")
	if err := os.MkdirAll(applicationsDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		return err
	}

	entryName := pkg.Title
	if strings.TrimSpace(entryName) == "" {
		entryName = pkg.Name
	}
	entryID := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(pkg.Name), " ", "-"))
	desktopEntry := fmt.Sprintf("[Desktop Entry]\nType=Application\nVersion=1.0\nName=%s\nExec=%q\nPath=%q\nTerminal=false\nCategories=Utility;Security;\nStartupNotify=true\n", entryName, launcher, installRoot)

	menuPath := filepath.Join(applicationsDir, entryID+".desktop")
	if err := os.WriteFile(menuPath, []byte(desktopEntry), 0o755); err != nil {
		return err
	}

	desktopPath := filepath.Join(desktopDir, entryName+".desktop")
	if err := os.WriteFile(desktopPath, []byte(desktopEntry), 0o755); err != nil {
		return err
	}
	return nil
}

func ensureWindowsShortcuts(pkg *api.ResolvedPackage, installRoot string) error {
	launcher := resolveLauncherPath(pkg, installRoot, "NeuralV.exe")
	if _, err := os.Stat(launcher); err != nil {
		return fmt.Errorf("launcher не найден после установки: %s", launcher)
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		return errors.New("APPDATA не задан")
	}
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		return errors.New("USERPROFILE не задан")
	}
	startMenu := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "NeuralV.lnk")
	desktop := filepath.Join(userProfile, "Desktop", "NeuralV.lnk")
	return createWindowsShortcuts(launcher, installRoot, startMenu, desktop)
}

func ensureWindowsCmdWrapper(installRoot, binaryName string) error {
	target := filepath.Join(installRoot, binaryName)
	wrapper := filepath.Join(installRoot, "nv.cmd")
	script := fmt.Sprintf("@echo off\r\n\"%s\" %%*\r\n", target)
	return os.WriteFile(wrapper, []byte(script), 0o755)
}

func ensureWindowsUserPath(installRoot string) error {
	currentPath := os.Getenv("PATH")
	if !pathListContains(currentPath, installRoot) {
		if currentPath == "" {
			_ = os.Setenv("PATH", installRoot)
		} else {
			_ = os.Setenv("PATH", installRoot+string(os.PathListSeparator)+currentPath)
		}
	}

	script := strings.Join([]string{
		fmt.Sprintf("$entry = '%s'", escapePowerShellString(installRoot)),
		"$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')",
		"$parts = @()",
		"if ($userPath) { $parts = $userPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries) }",
		"$exists = $false",
		"foreach ($part in $parts) { if ($part.TrimEnd('\\') -ieq $entry.TrimEnd('\\')) { $exists = $true; break } }",
		"if (-not $exists) {",
		"  $updated = @($entry)",
		"  if ($parts.Count -gt 0) { $updated += $parts }",
		"  [Environment]::SetEnvironmentVariable('Path', ($updated -join ';'), 'User')",
		"}",
	}, "\n")

	return runPowerShellScript(script)
}

func createWindowsShortcuts(target, workingDir string, shortcuts ...string) error {
	for _, item := range shortcuts {
		if err := os.MkdirAll(filepath.Dir(item), 0o755); err != nil {
			return err
		}
	}
	scriptLines := []string{
		"$WshShell = New-Object -ComObject WScript.Shell",
		fmt.Sprintf("$target = '%s'", escapePowerShellString(target)),
		fmt.Sprintf("$workingDir = '%s'", escapePowerShellString(workingDir)),
	}
	for index, shortcut := range shortcuts {
		scriptLines = append(scriptLines,
			fmt.Sprintf("$shortcutPath%d = '%s'", index, escapePowerShellString(shortcut)),
			fmt.Sprintf("$shortcut%d = $WshShell.CreateShortcut($shortcutPath%d)", index, index),
			fmt.Sprintf("$shortcut%d.TargetPath = $target", index),
			fmt.Sprintf("$shortcut%d.WorkingDirectory = $workingDir", index),
			fmt.Sprintf("$shortcut%d.Save()", index),
		)
	}
	return runPowerShellScript(strings.Join(scriptLines, "\n"))
}

func runPowerShellScript(script string) error {
	encoded := base64.StdEncoding.EncodeToString(utf16LEBytes(script))
	command := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func utf16LEBytes(text string) []byte {
	encoded := make([]byte, 0, len(text)*2)
	for _, r := range text {
		if r > 0xFFFF {
			r = '?'
		}
		encoded = append(encoded, byte(r), byte(r>>8))
	}
	return encoded
}

func escapePowerShellString(text string) string {
	return strings.ReplaceAll(text, "'", "''")
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

func pathListContains(pathEnv, target string) bool {
	if strings.TrimSpace(pathEnv) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	for _, item := range filepath.SplitList(pathEnv) {
		if sameFilePath(item, target) {
			return true
		}
	}
	return false
}

func warnIfNVUpdateAvailable(args []string, client *api.Client) {
	if shouldSkipNVUpdateCheck(args) {
		return
	}
	if semver.Validate(strings.TrimSpace(nvVersion)) != nil {
		return
	}

	latestVersion, err := latestPackageVersion(client, "nv", runtime.GOOS)
	if err != nil {
		return
	}
	if semver.Compare(latestVersion, nvVersion) <= 0 {
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

func latestPackageVersion(client *api.Client, packageName, goos string) (string, error) {
	details, err := client.PackageDetails(packageName, goos)
	if err == nil && details != nil {
		if !details.Success && strings.TrimSpace(details.Error) != "" {
			return "", errors.New(strings.TrimSpace(details.Error))
		}
		if latest, err := normalizePackageVersion(packageName, details.Package.LatestVersion); err == nil {
			return latest, nil
		}
	}

	resolved, err := client.ResolvePackage(packageName, "latest", goos, "")
	if err != nil {
		return "", err
	}
	if !resolved.Success && strings.TrimSpace(resolved.Error) != "" {
		return "", errors.New(strings.TrimSpace(resolved.Error))
	}
	return normalizePackageVersion(packageName, resolved.Package.ResolvedVersion)
}

func uninstallResolvedPackage(pkg *api.ResolvedPackage) error {
	switch pkg.Variant.UninstallStrategy {
	case "windows-remove-dir":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join(`%LOCALAPPDATA%`, pkg.Name))
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		fmt.Printf("Пакет %s удален из %s\n", pkg.Name, root)
		return nil
	case "linux-remove-dir":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "opt", pkg.Name))
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		fmt.Printf("Пакет %s удален из %s\n", pkg.Name, root)
		return nil
	case "linux-cli-wrapper-remove":
		root, err := resolveInstallRoot(pkg.Variant.InstallRoot, filepath.Join("$HOME", ".local", "share", pkg.Name))
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

func normalizePackageVersion(packageName, version string) (string, error) {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return "", fmt.Errorf("реестр пакета %s не содержит version", packageName)
	}
	normalized, err := semver.Normalize(trimmed)
	if err != nil {
		return "", fmt.Errorf("реестр пакета %s содержит некорректную версию %q", packageName, version)
	}
	return normalized, nil
}

func sameInstalledPackage(current, next api.ResolvedPackage) bool {
	return strings.EqualFold(current.Name, next.Name) &&
		current.ResolvedVersion == next.ResolvedVersion &&
		current.Variant.ID == next.Variant.ID
}

func printInstallTransition(current, next api.ResolvedPackage) {
	switch semver.Compare(next.ResolvedVersion, current.ResolvedVersion) {
	case 1:
		fmt.Printf("Обновляем %s: %s -> %s\n", next.Name, current.ResolvedVersion, next.ResolvedVersion)
	case -1:
		fmt.Printf("Меняем версию %s: %s -> %s\n", next.Name, current.ResolvedVersion, next.ResolvedVersion)
	default:
		fmt.Printf("Переустанавливаем %s %s\n", next.Name, next.ResolvedVersion)
	}
}

func daemonBinaryNameForPackage(pkg *api.ResolvedPackage, daemonURL string) string {
	if name, ok := metadataString(pkg.Variant.Metadata, "daemonBinaryName"); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if inferred := inferredBinaryName(daemonURL); inferred != "" {
		return inferred
	}
	return pkg.Name + "d"
}

func inferredBinaryName(downloadURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(downloadURL))
	if err != nil {
		return ""
	}

	name := path.Base(parsed.Path)
	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		name = strings.TrimSuffix(name, ".tar.gz")
	case strings.HasSuffix(name, ".tgz"):
		name = strings.TrimSuffix(name, ".tgz")
	case strings.HasSuffix(name, ".zip"):
		name = strings.TrimSuffix(name, ".zip")
	case strings.HasSuffix(name, ".exe"):
		name = strings.TrimSuffix(name, ".exe")
	}

	for _, suffix := range []string{"-linux", "-windows", "-darwin"} {
		marker := suffix + "-"
		index := strings.LastIndex(name, marker)
		if index <= 0 {
			continue
		}
		versionPart := name[index+len(marker):]
		if semver.Validate(versionPart) == nil {
			return name[:index]
		}
	}
	return name
}
