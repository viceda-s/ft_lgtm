package ipfs

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"
)

func testClient(t *testing.T) (*Client, string) {
    t.Helper()
    apiURL := os.Getenv("IPFS_TEST_API_URL")
    gatewayURL := os.Getenv("IPFS_TEST_GATEWAY_URL")
    if apiURL == "" || gatewayURL == "" {
        t.Skip("IPFS_TEST_API_URL and IPFS_TEST_GATEWAY_URL must be set")
    }
    client, err := NewClient(apiURL)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }
    return client, gatewayURL
}


func fetchFromGateway(t *testing.T, gatewayURL, cid, filename string) string {
    t.Helper()
    resp, err := http.Get(gatewayURL + "/ipfs/" + cid + "/" + filename)
    if err != nil {
        t.Fatalf("fetching %s from gateway: %v", filename, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, fetched %s, got %d", filename, resp.StatusCode)
    }
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        t.Fatalf("reading gateway response body: %v", err)
    }
    return string(body)
}


func TestUpload_ValidContent_ReturnsCIDAndRoundTrips(t *testing.T) {
    client, gatewayURL := testClient(t)

    source := "package main \n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
    stdout := "hello\n"
    stderr := ""

    cid, err := client.Upload(context.Background(), source, stdout, stderr)
    if err != nil {
        t.Fatalf("Upload returned error: %v", err)
    }
    if cid == "" {
        t.Fatalf("expected non-empty CID")
    }

    if got := fetchFromGateway(t, gatewayURL, cid, "main.go"); got != source {
        t.Fatalf("main.go round-trip mismatch: got %q, want %q", got, source)
    }
    if got := fetchFromGateway(t, gatewayURL, cid, "stdout.txt"); got != stdout {
        t.Fatalf("stdout.txt round-trip mismatch: got %q, want %q", got, stdout)
    }
    if got := fetchFromGateway(t, gatewayURL, cid, "stderr.txt"); !strings.EqualFold(got, stderr) {
        t.Fatalf("stderr.txt round-trip mismatch: got %q, want %q", got, stderr)
    }
}


func TestUpload_EmptyOutput_UploadsWithoutError(t *testing.T) {
    client, gatewayURL := testClient(t)

    cid, err := client.Upload(context.Background(), "package main\n", "", "")
    if err != nil {
        t.Fatalf("Upload returned error for empty stdout/stderr: %v", err)
    }
    if cid == "" {
        t.Fatalf("expected non-empty CID")
    }

    if got := fetchFromGateway(t, gatewayURL, cid, "stdout.txt"); got != "" {
        t.Fatalf("expected empty stdout.txt, got %q", got)
    }
}


func TestUpload_CancelledContext_ReturnsError(t *testing.T) {
    client, _ := testClient(t)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    _, err := client.Upload(ctx, "package main\n", "out", "")
    if err == nil {
        t.Fatalf("expected error for an already-cancelled context, got nil")
    }
}


func TestUpload_UnreachableAPI_ReturnsError(t *testing.T) {
    client, err := NewClient("http://127.0.0.1:1")
    if err != nil {
        t.Fatalf("NewClient failed unexpectedly: %v", err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    _, err = client.Upload(ctx, "package main\n", "out", "")
    if err == nil {
        t.Fatalf("expected error for unreachable Kubo API, got nil")
    }
}


func TestUpload_AddResponse_ReturnsWrapperDirectoryCID(t *testing.T) {
    cases := []struct {
        name    string
        body    string
        wantCID string
    }{
        {"wrapper after files", "{\"Name\":\"main.go\",\"Hash\":\"QmFile\"}\n{\"Name\":\"\",\"Hash\":\"QmDir\"}\n", "QmDir"},
        {"no wrapper", "{\"Name\":\"main.go\",\"Hash\":\"QmFile\"}\n", ""},
        {"two wrappers", "{\"Name\":\"\",\"Hash\":\"QmA\"}\n{\"Name\":\"\",\"Hash\":\"QmB\"}\n", ""},
        {"wrapper without hash", "{\"Name\":\"\",\"Hash\":\"\"}\n", ""},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            kubo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                _, _ = io.Copy(io.Discard, r.Body)
                w.Header().Set("Content-Type", "application/json")
                _, _ = io.WriteString(w, tc.body)
            }))
            defer kubo.Close()

            client, err := NewClient(kubo.URL)
            if err != nil {
                t.Fatalf("NewClient failed: %v", err)
            }
            cid, err := client.Upload(context.Background(), "package main\n", "out", "")
            if tc.wantCID == "" {
                if err == nil {
                    t.Fatalf("expected an error, got CID %q", cid)
                }
                return
            }
            if err != nil || cid != tc.wantCID {
                t.Fatalf("expected CID %q, got %q (err: %v)", tc.wantCID, cid, err)
            }
        })
    }
}
