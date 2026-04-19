package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Perdonus/NV/internal/api"
	"github.com/Perdonus/NV/internal/config"
)

func loginCommand(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", "", "")
	token := fs.String("token", "", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: nv login --token <token> [--server <url>]")
	}

	enteredToken := strings.TrimSpace(*token)
	if enteredToken == "" {
		return errors.New("не указан токен: nv login --token <token>")
	}

	cfg, err := loadNVConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = config.New()
	}
	clientBaseURL := resolvedBaseURL(defaultBaseURL)
	if strings.TrimSpace(*server) != "" {
		cfg.BaseURL = strings.TrimSpace(*server)
		clientBaseURL = cfg.BaseURL
	}

	client := api.NewClient(clientBaseURL)
	identity, err := client.WhoAmI(enteredToken)
	if err != nil {
		return fmt.Errorf("сервер не подтвердил токен: %w", err)
	}
	if identity == nil || !identity.Success {
		if identity != nil && strings.TrimSpace(identity.Error) != "" {
			return fmt.Errorf("сервер не подтвердил токен: %s", strings.TrimSpace(identity.Error))
		}
		return errors.New("сервер не подтвердил токен")
	}
	if strings.TrimSpace(identity.Identity) == "" {
		return errors.New("сервер не вернул identity для токена")
	}
	cfg.AuthToken = enteredToken
	fmt.Printf("Вход выполнен: %s\n", identity.Identity)
	return saveNVConfig(cfg)
}

func logoutCommand() error {
	cfg, err := loadNVConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = config.New()
	}
	cfg.AuthToken = ""
	if err := saveNVConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Токен удалён.")
	return nil
}

func whoamiCommand(client *api.Client) error {
	token := resolveAuthToken()
	if token == "" {
		return errors.New("токен не найден: используй nv login --token <token>")
	}
	response, err := client.WhoAmI(token)
	if err != nil {
		return err
	}
	if !response.Success {
		if strings.TrimSpace(response.Error) != "" {
			return errors.New(strings.TrimSpace(response.Error))
		}
		return errors.New("сервер не подтвердил токен")
	}
	if strings.TrimSpace(response.Identity) == "" {
		fmt.Println("Токен сохранён.")
		return nil
	}
	fmt.Println(strings.TrimSpace(response.Identity))
	return nil
}

func currentServerCommand() error {
	fmt.Println(resolvedBaseURL(defaultBaseURL))
	return nil
}
