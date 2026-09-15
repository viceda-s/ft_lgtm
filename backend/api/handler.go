package api

import (
	"context"
    "encoding/json"
    "errors"
	"log"
    "net/http"
    "strings"
	"time"

    "ft_lgtm/backend/compiler"
    "ft_lgtm/backend/executor"
)

const executionTimeout = 5 * time.Second
const maxOutputBytes = 10 * 1024


// uploadTimeout is the maximum time spent uploading execution results to IPFS. It is independent of executionTimeout and can be tuned separately.
const uploadTimeout = 5 * time.Second


// Uploader is satisfied by *ipfs.Client. It lives here (not in the ipfs package) so api can depend on it without ipfs needing to import api, and so handler tests can substitute a network-free fake.
type Uploader interface {
    Upload(ctx context.Context, source, stdout, stderr string) (string, error)
}


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
    IPFSLink     string `json:"ipfs_link,omitempty"`
}


type executeHandler struct {
    exec        *executor.Executor
    uploader    Uploader
    gatewayURL  string
}


// NewHandler returns the POST /api/execute HTTP handler.
// It compiles the submitted source with TinyGo, runs the result through exec inside its sandboxed Wasmtime store, uploads the source and output to IPFS via uploader, and reports the outcome as JSON.
// gatewayURL is the public IPFS gateway base (e.g. "http://ipfs.lgtm.local"), used to build IPFSLink.
func NewHandler(exec *executor.Executor, uploader Uploader, gatewayURL string) http.Handler {
    if uploader == nil {
        panic("api: NewHandler called with nil uploader")
    }
    return &executeHandler{exec: exec, uploader: uploader, gatewayURL: strings.TrimRight(gatewayURL, "/")}
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

    uploadCtx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
    defer cancel()
    cid, uploadErr := h.uploader.Upload(uploadCtx, req.Code, result.Stdout, result.Stderr)
    if uploadErr != nil {
        log.Printf("ipfs upload failed: %v", uploadErr)
    } else {
        resp.IPFSLink = h.gatewayURL + "/ipfs/" + cid
    }

    writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}