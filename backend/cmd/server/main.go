package main

import (
    "log"
    "net/http"
    "os"

    "ft_lgtm/backend/api"
    "ft_lgtm/backend/executor"
    "ft_lgtm/backend/ipfs"
)


func getEnvOrDefault(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func main() {
    ipfsAPIURL := getEnvOrDefault("IPFS_API_URL", "http://127.0.0.1:5001")
    ipfsGatewayURL := getEnvOrDefault("IPFS_GATEWAY_URL", "http://127.0.0.1:8080")

    ipfsClient, err := ipfs.NewClient(ipfsAPIURL)
    if err != nil {
        log.Fatal(err)
    }

    exec := executor.NewExecutor()
    mux := http.NewServeMux()
    mux.Handle("/api/execute", api.NewHandler(exec, ipfsClient, ipfsGatewayURL))

    addr := ":8080"
    log.Printf("listening on %s (ipfs api=%s gateway=%s)", addr, ipfsAPIURL, ipfsGatewayURL)
    if err := http.ListenAndServe(addr, mux); err != nil {
        log.Fatal(err)
    }
}