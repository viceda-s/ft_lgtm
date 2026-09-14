package main

import (
    "log"
    "net/http"

    "ft_lgtm/backend/api"
    "ft_lgtm/backend/executor"
)

func main() {
    exec := executor.NewExecutor()
    mux := http.NewServeMux()
    mux.Handle("/api/execute/", api.NewHandler(exec))

    addr := ":8080"
    log.Printf("listening on %s", addr)
    if err := http.ListenAndServe(addr, mux); err != nil {
        log.Fatalf(err)
    }
}