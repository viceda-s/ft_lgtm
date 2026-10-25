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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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

const compileTimeout = 20 * time.Second

const uploadTimeout = 5 * time.Second

const maxRequestBytes = 64 * 1024

const maxConcurrentRuns = 4

const instrumentationName = "ft_lgtm/backend/api"

func tracer() trace.Tracer { return otel.Tracer(instrumentationName) }

var executionOutcomes = []string{"success", "compile_error", "runtime_error", "timeout", "output_limit", "internal_error", "cancelled"}

var (
	instrumentsOnce             sync.Once
	executionsCounterInstrument metric.Int64Counter
	instrumentsErr              error
)

func instruments() (metric.Int64Counter, error) {
	instrumentsOnce.Do(func() {
		meter := otel.Meter(instrumentationName)
		executionsCounterInstrument, instrumentsErr = meter.Int64Counter(
			"code_executions_total",
			metric.WithDescription("Total number of code executions, labeled by outcome"),
		)
		if instrumentsErr != nil {
			return
		}
		for _, outcome := range executionOutcomes {
			executionsCounterInstrument.Add(context.Background(), 0, metric.WithAttributeSet(attribute.NewSet(attribute.String("outcome", outcome))))
		}
	})
	return executionsCounterInstrument, instrumentsErr
}

type Uploader interface {
	Upload(ctx context.Context, source, stdout, stderr string) (string, error)
}

type ExecuteRequest struct {
	Code string `json:"code"`
}

type ExecuteResponse struct {
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	Success         bool    `json:"success"`
	TimedOut        bool    `json:"timed_out"`
	OutputTruncated bool    `json:"output_truncated"`
	CompileError    string  `json:"compile_error,omitempty"`
	IPFSLink        string  `json:"ipfs_link,omitempty"`
	TraceID         string  `json:"trace_id,omitempty"`
	Timings         Timings `json:"timings_ms"`
}

type Timings struct {
	Compile    int64 `json:"compile"`
	Execute    int64 `json:"execute,omitempty"`
	IPFSUpload int64 `json:"ipfs_upload,omitempty"`
}

type executeHandler struct {
	exec       *executor.Executor
	uploader   Uploader
	gatewayURL string
	slots      chan struct{}
}

func NewHandler(exec *executor.Executor, uploader Uploader, gatewayURL string) http.Handler {
	if uploader == nil {
		panic("api: NewHandler called with nil uploader")
	}
	h := &executeHandler{
		exec:       exec,
		uploader:   uploader,
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		slots:      make(chan struct{}, maxConcurrentRuns),
	}
	instruments()
	return otelhttp.NewHandler(h, "/api/execute",
		otelhttp.WithSpanNameFormatter(func(operation string, _ *http.Request) string { return operation }))
}

func (h *executeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	span := trace.SpanFromContext(ctx)

	hash := sha256.Sum256([]byte(req.Code))
	codeHash := hex.EncodeToString(hash[:])
	span.SetAttributes(attribute.String("code.hash", codeHash))

	outcome := "success"
	defer func() { recordExecution(ctx, outcome) }()

	logger := telemetry.Logger(instrumentationName)

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		outcome = "cancelled"
		return
	}

	resp := ExecuteResponse{}
	if span.SpanContext().HasTraceID() {
		resp.TraceID = span.SpanContext().TraceID().String()
	}

	stageStart := time.Now()
	wasmBytes, err := h.compileWithSpan(ctx, req.Code)
	resp.Timings.Compile = time.Since(stageStart).Milliseconds()
	if err != nil {
		var compileErr *compiler.CompileError
		if !errors.As(err, &compileErr) {
			outcome = "internal_error"
			span.SetStatus(codes.Error, "compiler failed to run")
			logger.ErrorContext(ctx, "compiler failed to run", "code.hash", codeHash, "error", err)
			http.Error(w, "internal compilation error", http.StatusInternalServerError)
			return
		}
		outcome = "compile_error"
		span.SetStatus(codes.Error, "compile failed")
		resp.CompileError = compileErr.Stderr
		logger.ErrorContext(ctx, "compilation failed", "code.hash", codeHash, "compile_error", resp.CompileError)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	stageStart = time.Now()
	result, err := h.runWithSpan(ctx, wasmBytes)
	resp.Timings.Execute = time.Since(stageStart).Milliseconds()
	if err != nil {
		outcome = "internal_error"
		span.SetStatus(codes.Error, "execution error")
		logger.ErrorContext(ctx, "execution error", "code.hash", codeHash, "error", err)
		http.Error(w, "internal execution error", http.StatusInternalServerError)
		return
	}

	resp.Stdout = result.Stdout
	resp.Stderr = result.Stderr
	resp.TimedOut = result.TimedOut
	resp.OutputTruncated = result.OutputLimitExceeded
	resp.Success = !result.TimedOut && !result.OutputLimitExceeded && result.ExitError == nil

	switch {
	case result.TimedOut:
		outcome = "timeout"
		span.SetStatus(codes.Error, "execution timed out")
		logger.ErrorContext(ctx, "execution timed out", "code.hash", codeHash)
	case result.OutputLimitExceeded:
		outcome = "output_limit"
		span.SetStatus(codes.Error, "output limit exceeded")
		logger.ErrorContext(ctx, "execution stopped: output limit exceeded", "code.hash", codeHash, "limit_bytes", maxOutputBytes)
	case result.ExitError != nil:
		outcome = "runtime_error"
		span.SetStatus(codes.Error, result.ExitError.Error())
		logger.ErrorContext(ctx, "execution exited with error", "code.hash", codeHash, "error", result.ExitError)
	default:
		logger.InfoContext(ctx, "execution succeeded", "code.hash", codeHash)
	}

	stageStart = time.Now()
	cid, uploadErr := h.uploadWithSpan(ctx, req.Code, result.Stdout, result.Stderr)
	resp.Timings.IPFSUpload = time.Since(stageStart).Milliseconds()
	if uploadErr != nil {
		logger.ErrorContext(ctx, "ipfs upload failed", "code.hash", codeHash, "error", uploadErr)
	} else {
		resp.IPFSLink = h.gatewayURL + "/ipfs/" + cid
		span.SetAttributes(attribute.String("code.cid", cid))
	}

	writeJSON(w, http.StatusOK, resp)
}

func recordExecution(ctx context.Context, outcome string) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("execution.outcome", outcome))

	counter, err := instruments()
	if err != nil {
		telemetry.Logger(instrumentationName).ErrorContext(ctx, "constructing execution metrics", "error", err)
		return
	}
	counter.Add(ctx, 1, metric.WithAttributeSet(attribute.NewSet(attribute.String("outcome", outcome))))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *executeHandler) compileWithSpan(ctx context.Context, source string) ([]byte, error) {
	ctx, span := tracer().Start(ctx, "compile")
	defer span.End()

	compileCtx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()

	wasmBytes, err := compiler.Compile(compileCtx, source)
	if err != nil {
		span.SetStatus(codes.Error, "compile failed")
		return nil, err
	}
	span.SetAttributes(attribute.Int("wasm.size_bytes", len(wasmBytes)))
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
	span.SetAttributes(
		attribute.Int("execution.stdout_bytes", len(result.Stdout)),
		attribute.Int("execution.stderr_bytes", len(result.Stderr)),
	)
	switch {
	case result.TimedOut:
		span.SetStatus(codes.Error, "execution timed out")
	case result.OutputLimitExceeded:
		span.SetStatus(codes.Error, "output limit exceeded")
	case result.ExitError != nil:
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
