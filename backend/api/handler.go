package api

import (
    "encoding/json"
    "errors"
    "net/http"
    "time"

    "ft_lgtm/backend/compiler"
    "ft_lgtm/backend/executor"
)

const executionTimeout = 5 * time.Second
const maxOutputBytes = 10 * 1024


// ExecuteRequest is the JSON body for POST /api/execute
type ExecuteRequest struct {
    Code string `json:"code"`
}


// ExecuteResponse is the JSON body returned by POST /api/execute
type ExecuteResponse struct {
    Stdout       string `json:"stdout"`
    Stderr       string `json:"stderr"`
    Success      bool   `json:"success"`
    TimedOut     bool   `json:"timed_out"`
    CompileError string `json:"compile_error,omitempty"`
}


type executeHandler struct{
    exec *executor.Executor
}


// NewHandler returns the POST /api/execute HTTP handler. It compiles the submitted source with TinyGo, then runs the result through exec inside its sandboxed Wasmtime store, and reports the outcome as JSON.
func NewHandler(exec *executor.Executor) http.Handler {
    return &executeHandler{exec: exec}
}

func (h *executeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req ExecuteRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }

    wasmBytes, err := compiler.Compile(r.Context(), req.Code)
    if err != nil {
        resp := ExecuteResponse{Success: false}
        var compileErr *compiler.CompileError
        if errors.As(err, &compileErr) {
            resp.CompileError = compileErr.Stderr
        } else {
            resp.CompileError = err.Error()
        }
        writeJSON(w, http.StatusOK, resp)
        return
    }

    result, err := h.exec.Run(wasmBytes, executionTimeout, maxOutputBytes)
    if err != nil {
        http.Error(w, "internal execution error", http.StatusInternalServerError)
        return
    }

    resp := ExecuteResponse{
        Stdout:   result.Stdout,
        Stderr:   result.Stderr,
        TimedOut: result.TimedOut,
        Success:  !result.TimedOut && result.ExitError == nil,
    }
    writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}