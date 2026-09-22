package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"ft_lgtm/backend/compiler"
	"ft_lgtm/backend/executor"
	"ft_lgtm/backend/telemetry"
)

const executionTimeout = 5 * time.Second
const maxOutputBytes = 10 * 1024

// uploadTimeout is the maximum time spent uploading execution results to IPFS. It is independent of executionTimeout and can be tuned separately.
const uploadTimeout = 5 * time.Second

func tracer() trace.Tracer { return otel.Tracer("ft_lgtm/backend/api") }

var (
	executionsCounterOnce       sync.Once
	executionsCounterInstrument metric.Int64Counter
	executionsCounterErr        error
)

func executionsCounter() (metric.Int64Counter, error) {
	executionsCounterOnce.Do(func() {
		executionsCounterInstrument, executionsCounterErr = otel.Meter("ft_lgtm/backend/api").Int64Counter(
			"code_executions_total",
			metric.WithDescription("Total number of code executions, labeled by outcome"),
		)
	})
	return executionsCounterInstrument, executionsCounterErr
}

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
	exec       *executor.Executor
	uploader   Uploader
	gatewayURL string
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

	ctx, span := tracer().Start(r.Context(), "/api/execute")
	defer span.End()

	hash := sha256.Sum256([]byte(req.Code))
	codeHash := hex.EncodeToString(hash[:])
	span.SetAttributes(attribute.String("code.hash", codeHash))

	outcome := "success"
	defer func() {
		counter, err := executionsCounter()
		if err != nil {
			telemetry.Logger("ft_lgtm/backend/api").ErrorContext(ctx, "constructing executions counter", "error", err)
			return
		}
		counter.Add(ctx, 1, metric.WithAttributeSet(attribute.NewSet(attribute.String("outcome", outcome))))
	}()

	logger := telemetry.Logger("ft_lgtm/backend/api")

	wasmBytes, err := h.compileWithSpan(ctx, req.Code)
	if err != nil {
		outcome = "compile_error"
		span.SetStatus(codes.Error, "compile failed")
		resp := ExecuteResponse{Success: false}
		var compileErr *compiler.CompileError
		if errors.As(err, &compileErr) {
			resp.CompileError = compileErr.Stderr
		} else {
			resp.CompileError = err.Error()
		}
		logger.ErrorContext(ctx, "compilation failed", "code.hash", codeHash, "compile_error", resp.CompileError)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	result, err := h.runWithSpan(ctx, wasmBytes)
	if err != nil {
		outcome = "runtime_error"
		span.SetStatus(codes.Error, "execution error")
		logger.ErrorContext(ctx, "execution error", "code.hash", codeHash, "error", err)
		http.Error(w, "internal execution error", http.StatusInternalServerError)
		return
	}

	resp := ExecuteResponse{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		TimedOut: result.TimedOut,
		Success:  !result.TimedOut && result.ExitError == nil,
	}

	if result.TimedOut {
		outcome = "timeout"
		span.SetStatus(codes.Error, "execution timed out")
		logger.ErrorContext(ctx, "execution timed out", "code.hash", codeHash)
	} else if result.ExitError != nil {
		outcome = "runtime_error"
		span.SetStatus(codes.Error, result.ExitError.Error())
		logger.ErrorContext(ctx, "execution exited with error", "code.hash", codeHash, "error", result.ExitError)
	} else {
		logger.InfoContext(ctx, "execution succeeded", "code.hash", codeHash)
	}

	cid, uploadErr := h.uploadWithSpan(ctx, req.Code, result.Stdout, result.Stderr)
	if uploadErr != nil {
		logger.ErrorContext(ctx, "ipfs upload failed", "code.hash", codeHash, "error", uploadErr)
	} else {
		resp.IPFSLink = h.gatewayURL + "/ipfs/" + cid
		span.SetAttributes(attribute.String("code.cid", cid))
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *executeHandler) compileWithSpan(ctx context.Context, source string) ([]byte, error) {
	ctx, span := tracer().Start(ctx, "compile")
	defer span.End()

	wasmBytes, err := compiler.Compile(ctx, source)
	if err != nil {
		span.SetStatus(codes.Error, "compile failed")
		return nil, err
	}
	return wasmBytes, nil
}

func (h *executeHandler) runWithSpan(ctx context.Context, wasmBytes []byte) (executor.Result, error) {
	_, span := tracer().Start(ctx, "execute")
	defer span.End()

	result, err := h.exec.Run(wasmBytes, executionTimeout, maxOutputBytes)
	if err != nil {
		span.SetStatus(codes.Error, "execution error")
		return result, err
	}
	if result.TimedOut {
		span.SetStatus(codes.Error, "execution timed out")
	} else if result.ExitError != nil {
		span.SetStatus(codes.Error, result.ExitError.Error())
	}
	return result, nil
}

func (h *executeHandler) uploadWithSpan(ctx context.Context, source, stdout, stderr string) (string, error) {
	ctx, span := tracer().Start(ctx, "ipfs_upload")
	defer span.End()

	uploadCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()

	cid, err := h.uploader.Upload(uploadCtx, source, stdout, stderr)
	if err != nil {
		span.SetStatus(codes.Error, "ipfs upload failed")
		return "", err
	}
	span.SetAttributes(attribute.String("code.cid", cid))
	return cid, nil
}
