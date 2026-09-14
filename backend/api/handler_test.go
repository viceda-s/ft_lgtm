package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ft_lgtm/backend/executor"
)


func TestHandler_ValidCode_ReturnsStdoutAndSuccess(t *testing.T) {
    exec := executor.NewExecutor()
    handler := NewHandler(exec)

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
    handler := NewHandler(exec)

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
	handler := NewHandler(exec)

	req := httptest.NewRequest(http.MethodPost, "/api/execute/", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}
