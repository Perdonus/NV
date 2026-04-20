package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Perdonus/NV/internal/api"
)

func viewCommand(client *api.Client, args []string) error {
	jsonOutput, targetOS, positional, err := parseViewArgs(args)
	if err != nil {
		return err
	}
	if len(positional) < 1 {
		return errors.New("не хватает имени пакета: view <package[@version|tag]>")
	}

	name, version, err := parsePackageSpec(positional[0])
	if err != nil {
		return err
	}
	response, err := client.ViewPackage(name, version, targetOS)
	if err != nil {
		return fmt.Errorf("реестр пакетов недоступен: %w", err)
	}
	if !response.Success {
		if strings.TrimSpace(response.Error) != "" {
			return errors.New(strings.TrimSpace(response.Error))
		}
		return errors.New("сервер не вернул данные пакета")
	}

	fields := positional[1:]
	if len(fields) == 0 {
		if jsonOutput {
			return printJSON(response.Package)
		}
		printPackageView(response.Package)
		return nil
	}

	root, err := toMap(response.Package)
	if err != nil {
		return err
	}
	if len(fields) == 1 {
		value, err := selectViewField(root, fields[0])
		if err != nil {
			return err
		}
		return printViewValue(value, jsonOutput)
	}

	results := map[string]any{}
	for _, field := range fields {
		value, err := selectViewField(root, field)
		if err != nil {
			return err
		}
		results[field] = value
	}
	return printViewValue(results, jsonOutput)
}

func parseViewArgs(args []string) (bool, string, []string, error) {
	jsonOutput := false
	targetOS := "all"
	positional := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		switch {
		case argument == "":
			continue
		case argument == "--json":
			jsonOutput = true
		case argument == "--os":
			if index+1 >= len(args) {
				return false, "", nil, fmt.Errorf("usage: nv view <package[@version|tag]> [field[.subfield]...] [--json] [--os <linux|windows|all>]")
			}
			targetOS = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(argument, "--os="):
			targetOS = strings.TrimSpace(strings.TrimPrefix(argument, "--os="))
		case strings.HasPrefix(argument, "--"):
			return false, "", nil, fmt.Errorf("usage: nv view <package[@version|tag]> [field[.subfield]...] [--json] [--os <linux|windows|all>]")
		default:
			positional = append(positional, argument)
		}
	}
	if targetOS == "" {
		targetOS = "all"
	}
	return jsonOutput, targetOS, positional, nil
}

func printPackageView(view api.PackageView) {
	fmt.Printf("Name: %s\n", view.Name)
	if title := strings.TrimSpace(view.Title); title != "" {
		fmt.Printf("Title: %s\n", title)
	}
	if description := strings.TrimSpace(view.Description); description != "" {
		fmt.Printf("Description: %s\n", description)
	}
	if homepage := strings.TrimSpace(view.Homepage); homepage != "" {
		fmt.Printf("Homepage: %s\n", homepage)
	}
	if latest := strings.TrimSpace(view.LatestVersion); latest != "" {
		fmt.Printf("Latest: %s\n", latest)
	}
	if len(view.DistTags) > 0 {
		fmt.Println("Dist-tags:")
		for tag, version := range view.DistTags {
			fmt.Printf("  %s: %s\n", tag, version)
		}
	}
	if len(view.Versions) > 0 {
		fmt.Printf("Versions: %s\n", strings.Join(view.Versions, ", "))
	}
	if selected := strings.TrimSpace(view.Version.Version); selected != "" {
		fmt.Printf("Selected: %s\n", selected)
	}
	if readme := strings.TrimSpace(view.Version.Readme); readme != "" {
		fmt.Println("Readme:")
		fmt.Println(readme)
	}
	if len(view.Version.Variants) > 0 {
		fmt.Println("Variants:")
		for _, variant := range view.Version.Variants {
			line := fmt.Sprintf("  %s", variant.ID)
			if variant.Label != "" {
				line += fmt.Sprintf(" %s", variant.Label)
			}
			if variant.OS != "" {
				line += fmt.Sprintf(" os=%s", variant.OS)
			}
			if variant.FileName != "" {
				line += fmt.Sprintf(" file=%s", variant.FileName)
			}
			if variant.IsDefault || variant.Default {
				line += " default"
			}
			fmt.Println(line)
		}
	}
}

func selectViewField(root map[string]any, field string) (any, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(field), "-", "_")
	if normalized == "" {
		return nil, errors.New("поле не указано")
	}
	current := any(root)
	for _, part := range strings.Split(normalized, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("поле %s не найдено", field)
		}
		value, exists := object[part]
		if !exists {
			return nil, fmt.Errorf("поле %s не найдено", field)
		}
		current = value
	}
	return current, nil
}

func printViewValue(value any, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(value)
	}
	switch typed := value.(type) {
	case string:
		fmt.Println(strings.TrimSpace(typed))
	case []any:
		for _, item := range typed {
			fmt.Println(renderViewScalar(item))
		}
	case []string:
		for _, item := range typed {
			fmt.Println(item)
		}
	case map[string]any:
		return printJSON(typed)
	default:
		fmt.Println(renderViewScalar(typed))
	}
	return nil
}

func renderViewScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		payload, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(payload)
	}
}

func toMap(value any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func printJSON(value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}
