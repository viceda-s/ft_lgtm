package ipfs

import (
    "context"
    "io"
    "net/http"
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
    // This test does not require IPFS_TEST_API_URL - it deliberately targets a port nothing is listening on, so it runs unconditionally.
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