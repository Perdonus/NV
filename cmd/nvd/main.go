package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Perdonus/NV/internal/backend"
)

func main() {
	var (
		addr          = flag.String("addr", envOr("NVD_ADDR", ":8080"), "listen address")
		dataDir       = flag.String("data-dir", envOr("NVD_DATA_DIR", filepath.Join(".", "var", "nvd")), "storage directory")
		seedPath      = flag.String("seed", envOr("NVD_SEED_PATH", defaultSeedPath()), "seed catalog path")
		publicBaseURL = flag.String("public-base-url", envOr("NVD_PUBLIC_BASE_URL", ""), "public base URL for absolute links")
		publishToken  = flag.String("publish-token", envOr("NVD_PUBLISH_TOKEN", ""), "publisher bearer token")
	)
	flag.Parse()

	service, err := backend.NewService(backend.Config{
		DataDir:       *dataDir,
		SeedPath:      *seedPath,
		PublicBaseURL: *publicBaseURL,
		PublishToken:  *publishToken,
	})
	if err != nil {
		log.Fatalf("nvd init failed: %v", err)
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           service.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("nvd listening on %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("nvd server failed: %v", err)
		}
	}()

	<-stop
	log.Printf("nvd shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultSeedPath() string {
	candidates := []string{
		filepath.Join(".", "registry", "packages.json"),
		filepath.Join(".", "packages.seed.json"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}
