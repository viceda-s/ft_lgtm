package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ft_lgtm/backend/executor"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandler_ValidCode_ReturnsStdoutAndSuccess(t *testing.T) {
	exec := executor.NewExecutor()
	handler := NewHandler(exec, &fakeUploader{cid: "bafyFAKECID"}, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\n func main() {\n\tprintln(\"Hello\")\n\t}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got response: %+v", resp)
	}

	if resp.Stdout == "" {
		t.Fatal("expected non-empty stdout")
	}
}

func TestHandler_InvalidCode_ReturnsCompileError(t *testing.T) {
	exec := executor.NewExecutor()
	handler := NewHandler(exec, &fakeUploader{cid: "bafyFAKECID"}, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tthis is not valid syntax\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 (errors are reported in the body, not via HTTP status), got %d", rec.Code)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Success {
		t.Fatal("expected Success=false for invalid syntax")
	}
	if resp.CompileError == "" {
		t.Fatal("expected non-empty CompileError")
	}
}

func TestHandler_MalformedJSON_Returns400(t *testing.T) {
	exec := executor.NewExecutor()
	handler := NewHandler(exec, &fakeUploader{cid: "bafyFAKECID"}, "http://ipfs.lgtm.local")

	req := httptest.NewRequest(http.MethodPost, "/api/execute/", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

type fakeUploader struct {
	cid string
	err error
}

func (f *fakeUploader) Upload(ctx context.Context, source, stdout, stderr string) (string, error) {
	return f.cid, f.err
}

func TestHandler_ValidCode_IncludesIPFSLink(t *testing.T) {
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"Hello, IPFS\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	want := "http://ipfs.lgtm.local/ipfs/bafyFAKECID"
	if resp.IPFSLink != want {
		t.Fatalf("expected IPFSLink %q, got %q", want, resp.IPFSLink)
	}
}

func TestHandler_RuntimeFailure_StillIncludesIPFSLink(t *testing.T) {
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	//Compiles fine, but panics at runtime
	body := `{"code": "package main\n\nfunc main() {\n\tvar s []int\n\t_ = s[5]\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Success {
		t.Fatal("expected Success=false for a runtime panic")
	}
	if resp.IPFSLink == "" {
		t.Fatal("expected non-empty IPFSLink for a runtime failure (execution still ran)")
	}
}

func TestHandler_Timeout_StillIncludesIPFSLink(t *testing.T) {
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tfor {}\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.TimedOut {
		t.Fatal("expected TimedOut=true for an infinite loop")
	}
	if resp.IPFSLink == "" {
		t.Fatal("expected non-empty IPFSLink for a timed-out execution")
	}
}

func TestHandler_InvalidCode_HasNoIPFSLink(t *testing.T) {
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tthis is not valid syntax\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.IPFSLink != "" {
		t.Fatalf("expected empty IPFSLink for a compile error, got %q", resp.IPFSLink)
	}
}

func TestHandler_UploadFails_Returns200WithoutIPFSLink(t *testing.T) {
	exec := executor.NewExecutor()
	uploader := &fakeUploader{err: errors.New("connection refused")}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 even when IPFS upload fails, got %d", rec.Code)
	}

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected Success=true - execution succeeded even though upload failed")
	}
	if resp.Stdout == "" {
		t.Fatal("expected non-empty stdout even though upload failed")
	}
	if resp.IPFSLink != "" {
		t.Fatalf("expected no IPFSLink when upload fails, got %q", resp.IPFSLink)
	}
}

// withTestTracerProvider installs an in-memory span recorder as the global TracerProvider for the duration of one test, restoring the previous global provider afterward so tests don't leak state into each other.
func withTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

func TestHandler_ValidCode_RecordsRootSpan(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var root sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "/api/execute" {
			root = s
		}
	}
	if root == nil {
		t.Fatalf("expected a span named \"/api/execute\" among %d recorded spans", len(spans))
	}
	if root.Status().Code == codes.Error {
		t.Fatalf("expected root span status to not be Error for a successful execution, got %s", root.Status().Description)
	}
}

func TestHandler_InvalidCode_RootSpanHasErrorStatus(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tthis is not a valid syntax\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var root sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "/api/execute" {
			root = s
		}
	}
	if root == nil {
		t.Fatal("expected a span named \"/api/execute\"")
	}
	if root.Status().Code != codes.Error {
		t.Fatalf("expected root span status Error for a compile failure, got %s", root.Status().Code)
	}
}

func TestHandler_InvalidCode_RootSpanHasCodeHash(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tthis is not a valid syntax\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var root sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "/api/execute" {
			root = s
		}
	}
	if root == nil {
		t.Fatal("expected a span named \"/api/execute\"")
	}
	found := false
	for _, attr := range root.Attributes() {
		if attr.Key == "code.hash" && attr.Value.AsString() != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected root span status to carry a non-empty cose.hash attribute even for a compile error")
	}
}

func TestHandler_ValidCode_RecordsCompileChildSpan(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var root, compile sdktrace.ReadOnlySpan
	for _, s := range spans {
		switch s.Name() {
		case "/api/execute":
			root = s
		case "compile":
			compile = s
		}
	}
	if root == nil {
		t.Fatal("expected a span named \"/api/execute\"")
	}
	if compile == nil {
		t.Fatalf("expected a span named \"compile\" among %d recorded spans", len(spans))
	}
	if compile.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Fatalf("expected \"compile\" span's parent to be then root span")
	}
}

func TestHandler_ValidCode_RecordsExecuteChildSpan(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var execute sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "execute" {
			execute = s
		}
	}
	if execute == nil {
		t.Fatalf("expected a span named \"execute\" among %d recorded spans", len(spans))
	}
	if execute.Status().Code == codes.Error {
		t.Fatalf("expected \"execute\" span status not to be Error for a compile successful run, got: %s", execute.Status().Description)
	}
}

func TestHandler_RuntimeFailure_ExecuteSpanHasErrorStatus(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tvar s []int\n\t_ = s[5]\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var execute sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "execute" {
			execute = s
		}
	}
	if execute == nil {
		t.Fatal("expected a span named \"/api/execute\"")
	}
	if execute.Status().Code != codes.Error {
		t.Fatalf("expected \"execute\" span status not to be Error for a runtime panic, got: %s", execute.Status().Code)
	}
}

func TestHandler_Timeout_ExecuteSpanHasErrorStatus(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tfor {}\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var execute sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "execute" {
			execute = s
		}
	}
	if execute == nil {
		t.Fatal("expected a span named \"/api/execute\"")
	}
	if execute.Status().Code != codes.Error {
		t.Fatalf("expected \"execute\" span status not to be Error for a timeout: %s", execute.Status().Code)
	}
}

func TestHandler_ValidCode_RecordsIPFSUploadChildSpanWithCID(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var upload sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "ipfs_upload" {
			upload = s
		}
	}
	if upload == nil {
		t.Fatalf("expected a span named \"ipfs_upload\" among %d recorded spans", len(spans))
	}
	found := false
	for _, attr := range upload.Attributes() {
		if attr.Key == "code.cid" && attr.Value.AsString() == "bafyFAKECID" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected \"ipfs_upload\" span to carry code.cid=bafyFAKECID attribute")
	}
}

func TestHandler_UploadFails_IPFSUploadSpanHasErrorStatus(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{err: errors.New("connection refused")}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	var upload sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "ipfs_upload" {
			upload = s
		}
	}
	if upload == nil {
		t.Fatal("expected a span named \"ipfs_upload\"")
	}
	if upload.Status().Code != codes.Error {
		t.Fatalf("expected \"ipfs_upload\" span status  Error when upload fails, got: %s", upload.Status().Code)
	}
}

func TestHandler_InvalidCode_NoIPFSUploadSpan(t *testing.T) {
	recorder := withTestTracerProvider(t)
	exec := executor.NewExecutor()
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(exec, uploader, "http://ipfs.lgtm.local")

	body := `{"code": "package main\n\nfunc main() {\n\tthis is not a valid syntax\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	for _, s := range spans {
		if s.Name() == "ipfs_upload" {
			t.Fatal("expected no \"ipfs_upload\" span for a compile error")
		}
	}
}
