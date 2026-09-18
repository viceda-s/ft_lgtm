package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ft_lgtm/backend/api"
	"ft_lgtm/backend/executor"
	"ft_lgtm/backend/ipfs"
	"ft_lgtm/backend/telemetry"
)

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := telemetry.Init(ctx, "ft_lgtm-backend")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	ipfsAPIURL := getEnvOrDefault("IPFS_API_URL", "http://127.0.0.1:5001")
	ipfsGatewayURL := getEnvOrDefault("IPFS_GATEWAY_URL", "http://127.0.0.1:8080")

	ipfsClient, err := ipfs.NewClient(ipfsAPIURL)
	if err != nil {
		log.Print(err)
		return
	}

	exec := executor.NewExecutor()
	mux := http.NewServeMux()
	mux.Handle("/api/execute", api.NewHandler(exec, ipfsClient, ipfsGatewayURL))

	addr := ":8080"
	log.Printf("listening on %s (ipfs api=%s gateway=%s otlp endpoint=%s)",
		addr, ipfsAPIURL, ipfsGatewayURL, getEnvOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"))
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Print(err)
	}
}
