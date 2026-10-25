package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"ft_lgtm/backend/executor"
	"ft_lgtm/backend/ipfs"
)

type fakeSharedCode struct {
	file   string
	cid    string
	code   string
	err    error
	called bool
}

func (f *fakeSharedCode) SharedCode(ctx context.Context, id string) (string, string, string, error) {
	f.called = true
	return f.cid, f.file, f.code, f.err
}

func serveShare(reader SharedCodeReader, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /api/share/{id}", NewShareHandler(reader, "http://ipfs.lgtm.local/"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestShareHandler_KnownID_ReturnsCodeAndIPFSLink(t *testing.T) {
	reader := &fakeSharedCode{cid: "QmCodeDir", file: "main.rs", code: "fn main() {}\n"}

	rec := serveShare(reader, "/api/share/abcdefgh")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ShareResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != "fn main() {}\n" || resp.Language != "rust" || resp.IPFSLink != "http://ipfs.lgtm.local/ipfs/QmCodeDir" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestShareHandler_InvalidID_Returns400WithoutLookup(t *testing.T) {
	for _, id := range []string{"short", "ABCDEFGH", "abcdefgh1", "abcdefghijk", "abcd%2F..%2Fx"} {
		reader := &fakeSharedCode{}

		rec := serveShare(reader, "/api/share/"+id)

		if rec.Code != http.StatusBadRequest || reader.called {
			t.Fatalf("id %q: expected 400 without a Kubo lookup, got %d (lookup: %v)", id, rec.Code, reader.called)
		}
	}
}

func TestShareHandler_UnknownID_Returns404(t *testing.T) {
	rec := serveShare(&fakeSharedCode{err: ipfs.ErrShareNotFound}, "/api/share/abcdefghabcdefgh")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestShareHandler_KuboFails_Returns500AndErrorSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	rec := serveShare(&fakeSharedCode{err: fmt.Errorf("kubo down")}, "/api/share/abcdefgh")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "/api/share/{id}" || spans[0].Status().Code != codes.Error {
		t.Fatalf("expected one errored /api/share/{id} span, got %d spans", len(spans))
	}
}

func executeWith(t *testing.T, uploader *fakeUploader) ExecuteResponse {
	t.Helper()
	handler := NewHandler(executor.NewExecutor(), uploader, "http://ipfs.lgtm.local")
	body := `{"code": "package main\n\nfunc main() {\n\tprintln(\"share me\")\n}\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var resp ExecuteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestHandler_ValidCode_IncludesShareID(t *testing.T) {
	resp := executeWith(t, &fakeUploader{cid: "bafyFAKECID", shareID: "abcdefgh"})

	if resp.ShareID != "abcdefgh" {
		t.Fatalf("expected share_id abcdefgh, got %q", resp.ShareID)
	}
}

func TestHandler_ShareFails_KeepsIPFSLinkWithoutShareID(t *testing.T) {
	resp := executeWith(t, &fakeUploader{cid: "bafyFAKECID", shareErr: errors.New("mfs full")})

	if resp.ShareID != "" || resp.IPFSLink == "" || !resp.Success {
		t.Fatalf("expected a successful run with an IPFS link and no share_id, got %+v", resp)
	}
}
