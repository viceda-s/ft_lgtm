package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ft_lgtm/backend/compiler"
	"ft_lgtm/backend/executor"
)

func TestMain(m *testing.M) {
	compiler.RunSandboxIfRequested()
	os.Exit(m.Run())
}

func TestHandler_UnsupportedLanguage_Returns400(t *testing.T) {
	handler := NewHandler(executor.NewExecutor(), &fakeUploader{cid: "bafyFAKECID"}, "http://ipfs.lgtm.local")
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(`{"code":"x","language":"cobol"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported language, got %d", rec.Code)
	}
}

func TestHandler_RustCode_RunsAndUploadsMainRs(t *testing.T) {
	if _, err := os.Stat("/opt/rust/bin/rustc"); err != nil {
		t.Skip("rust toolchain not installed")
	}
	uploader := &fakeUploader{cid: "bafyFAKECID"}
	handler := NewHandler(executor.NewExecutor(), uploader, "http://ipfs.lgtm.local")
	body := `{"language":"rust","code":"fn main() { println!(\"rust says {}\", 6 * 7); }"}`
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body)))

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success || resp.Stdout != "rust says 42\n" {
		t.Fatalf("expected the Rust program to print, got %+v", resp)
	}
	if uploader.file != "main.rs" {
		t.Fatalf("expected the source to be uploaded as main.rs, got %q", uploader.file)
	}
}
