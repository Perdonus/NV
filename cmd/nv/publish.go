package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Perdonus/NV/internal/api"
)

func packCommand(args []string) error {
	var manifestPath string
	var outputPath string
	positional := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		switch {
		case argument == "":
			continue
		case argument == "--manifest":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv pack [--manifest <file>] [--out <file>]")
			}
			manifestPath = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--manifest="):
			manifestPath = strings.TrimSpace(strings.TrimPrefix(argument, "--manifest="))
		case argument == "--out":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv pack [--manifest <file>] [--out <file>]")
			}
			outputPath = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--out="):
			outputPath = strings.TrimSpace(strings.TrimPrefix(argument, "--out="))
		case strings.HasPrefix(argument, "--"):
			return fmt.Errorf("usage: nv pack [--manifest <file>] [--out <file>]")
		default:
			positional = append(positional, argument)
		}
	}
	candidate := strings.TrimSpace(manifestPath)
	if candidate == "" && len(positional) > 0 {
		candidate = positional[0]
	}
	loaded, err := loadPackageManifest(candidate)
	if err != nil {
		return err
	}

	out := strings.TrimSpace(outputPath)
	if out == "" {
		out = fmt.Sprintf("%s-%s.nvpack.tgz", safeFilesystemToken(loaded.Manifest.Name), loaded.Manifest.Version)
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := writePackArchive(out, loaded); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func publishCommand(client *api.Client, args []string) error {
	var manifestPath string
	var server string
	var token string
	var tag string
	var notes string
	dryRun := false
	positional := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		switch {
		case argument == "":
			continue
		case argument == "--manifest":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
			}
			manifestPath = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--manifest="):
			manifestPath = strings.TrimSpace(strings.TrimPrefix(argument, "--manifest="))
		case argument == "--server":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
			}
			server = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--server="):
			server = strings.TrimSpace(strings.TrimPrefix(argument, "--server="))
		case argument == "--token":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
			}
			token = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--token="):
			token = strings.TrimSpace(strings.TrimPrefix(argument, "--token="))
		case argument == "--tag":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
			}
			tag = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--tag="):
			tag = strings.TrimSpace(strings.TrimPrefix(argument, "--tag="))
		case argument == "--notes":
			if index+1 >= len(args) {
				return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
			}
			notes = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--notes="):
			notes = strings.TrimSpace(strings.TrimPrefix(argument, "--notes="))
		case argument == "--dry-run":
			dryRun = true
		case strings.HasPrefix(argument, "--"):
			return fmt.Errorf("usage: nv publish [--manifest <file>] [--tag <tag>] [--dry-run] [--token <token>] [--server <url>]")
		default:
			positional = append(positional, argument)
		}
	}

	candidate := strings.TrimSpace(manifestPath)
	if candidate == "" && len(positional) > 0 {
		candidate = positional[0]
	}
	loaded, err := loadPackageManifest(candidate)
	if err != nil {
		return err
	}
	if tagValue := strings.TrimSpace(tag); tagValue != "" {
		loaded.Manifest.DistTags = append(loaded.Manifest.DistTags, tagValue)
		loaded.Manifest.DistTags = dedupeStrings(loaded.Manifest.DistTags)
	}
	if notesPath := strings.TrimSpace(notes); notesPath != "" {
		resolved := filepath.Join(loaded.Dir, filepath.FromSlash(notesPath))
		if _, err := os.Stat(resolved); err != nil {
			return fmt.Errorf("notes не найден: %s", notesPath)
		}
		loaded.NotesPath = resolved
	}

	request, _, err := loaded.publishRequest(dryRun)
	if err != nil {
		return err
	}

	targetClient := client
	if override := strings.TrimSpace(server); override != "" {
		targetClient = api.NewClient(override)
	}
	authToken := strings.TrimSpace(token)
	if authToken == "" {
		authToken = resolveAuthToken()
	}
	if authToken == "" {
		return errors.New("токен публикации не найден: используй nv login --token <token> или NV_AUTH_TOKEN")
	}

	response, err := targetClient.PublishPackage(request, authToken)
	if err != nil {
		return fmt.Errorf("публикация не выполнена: %w", err)
	}
	if !response.Success {
		if strings.TrimSpace(response.Error) != "" {
			return errors.New(strings.TrimSpace(response.Error))
		}
		return errors.New("сервер не подтвердил публикацию")
	}

	status := "Опубликовано"
	if dryRun {
		status = "Проверено без публикации"
	}
	fmt.Printf("%s: %s %s\n", status, response.Package.Name, response.Package.Version.Version)
	if len(response.Package.DistTags) > 0 {
		fmt.Println("Dist-tags:")
		for tagName, version := range response.Package.DistTags {
			fmt.Printf("  %s: %s\n", tagName, version)
		}
	}
	for _, variant := range response.Package.Version.Variants {
		if strings.TrimSpace(variant.DownloadURL) == "" {
			continue
		}
		fmt.Printf("  %s -> %s\n", variant.ID, variant.DownloadURL)
	}
	return nil
}

func writePackArchive(target string, loaded *loadedPackageManifest) error {
	output, err := os.Create(target)
	if err != nil {
		return err
	}
	defer output.Close()

	gzipWriter := gzip.NewWriter(output)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	manifestPayload, _, err := loaded.publishRequest(false)
	if err != nil {
		return err
	}
	if err := addTarBytes(tarWriter, "manifest.json", manifestPayload.Manifest, 0o644); err != nil {
		return err
	}
	if loaded.ReadmePath != "" {
		if err := addTarFile(tarWriter, loaded.ReadmePath, "README.md"); err != nil {
			return err
		}
	}
	if loaded.NotesPath != "" {
		if err := addTarFile(tarWriter, loaded.NotesPath, "NOTES.md"); err != nil {
			return err
		}
	}
	for _, variant := range loaded.Manifest.Variants {
		if err := addTarFile(tarWriter, loaded.ArtifactPath[variant.ID], filepath.ToSlash(filepath.Join("artifacts", variant.ID, variant.FileName))); err != nil {
			return err
		}
	}
	return nil
}

func addTarFile(writer *tar.Writer, sourcePath, targetPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name: filepath.ToSlash(targetPath),
		Mode: 0o644,
		Size: info.Size(),
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func addTarBytes(writer *tar.Writer, targetPath string, payload []byte, mode int64) error {
	if err := writer.WriteHeader(&tar.Header{
		Name: filepath.ToSlash(targetPath),
		Mode: mode,
		Size: int64(len(payload)),
	}); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
