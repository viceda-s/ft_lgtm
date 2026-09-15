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