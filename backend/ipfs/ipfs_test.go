package ipfs

import (
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
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


func fakeKubo(t *testing.T, mfs map[string]string, sources map[string]string) *Client {
    t.Helper()
    kubo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        args := r.URL.Query()["arg"]
        w.Header().Set("Content-Type", "application/json")
        kuboError := func(msg string) {
            w.WriteHeader(http.StatusInternalServerError)
            _, _ = io.WriteString(w, `{"Message":"`+msg+`","Code":0,"Type":"error"}`)
        }
        switch r.URL.Path {
        case "/api/v0/files/cp":
            if r.URL.Query().Get("parents") != "true" {
                t.Errorf("files/cp called without parents=true")
            }
            if _, taken := mfs[args[1]]; taken {
                kuboError("cp: cannot put node in path " + args[1] + ": directory already has entry by that name")
                return
            }
            mfs[args[1]] = strings.TrimPrefix(args[0], "/ipfs/")
            _, _ = io.WriteString(w, "")
        case "/api/v0/files/stat":
            cid, ok := mfs[args[0]]
            if !ok {
                kuboError("file does not exist")
                return
            }
            _, _ = io.WriteString(w, `{"Hash":"`+cid+`","Type":"directory"}`)
        case "/api/v0/cat":
            source, ok := sources[args[0]]
            if !ok {
                kuboError("no link named \"main.go\"")
                return
            }
            w.Header().Set("Content-Type", "text/plain")
            _, _ = io.WriteString(w, source)
        default:
            t.Errorf("unexpected Kubo call %s", r.URL.Path)
            w.WriteHeader(http.StatusNotFound)
        }
    }))
    t.Cleanup(kubo.Close)

    client, err := NewClient(kubo.URL)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }
    return client
}


func TestShare_NewCID_CreatesShortLinkInMFS(t *testing.T) {
    mfs := map[string]string{}
    client := fakeKubo(t, mfs, nil)

    id, err := client.Share(context.Background(), "QmCodeDir")
    if err != nil {
        t.Fatalf("Share failed: %v", err)
    }
    if len(id) != 8 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz234567") != "" {
        t.Fatalf("expected an 8-char lowercase base32 id, got %q", id)
    }
    if mfs["/shares/"+id] != "QmCodeDir" {
        t.Fatalf("expected /shares/%s to point at QmCodeDir, MFS is %v", id, mfs)
    }
}


func TestShare_SameCIDTwice_ReturnsSameID(t *testing.T) {
    client := fakeKubo(t, map[string]string{}, nil)

    first, err := client.Share(context.Background(), "QmCodeDir")
    if err != nil {
        t.Fatalf("first Share failed: %v", err)
    }
    second, err := client.Share(context.Background(), "QmCodeDir")
    if err != nil || second != first {
        t.Fatalf("expected the same id %q for the same CID, got %q (err: %v)", first, second, err)
    }
}


func TestShare_ShortIDTakenByOtherCID_UsesLongerID(t *testing.T) {
    mfs := map[string]string{"/shares/" + shareID("QmCodeDir", 8): "QmSomethingElse"}
    client := fakeKubo(t, mfs, nil)

    id, err := client.Share(context.Background(), "QmCodeDir")
    if err != nil {
        t.Fatalf("Share failed: %v", err)
    }
    if id != shareID("QmCodeDir", 16) || mfs["/shares/"+id] != "QmCodeDir" {
        t.Fatalf("expected the 16-char id to point at QmCodeDir, got %q with MFS %v", id, mfs)
    }
}


func TestSharedCode_KnownID_ReturnsCIDAndSource(t *testing.T) {
    mfs := map[string]string{"/shares/abcdefgh": "QmCodeDir"}
    sources := map[string]string{"/ipfs/QmCodeDir/main.go": "package main\n"}
    client := fakeKubo(t, mfs, sources)

    cid, source, err := client.SharedCode(context.Background(), "abcdefgh")
    if err != nil {
        t.Fatalf("SharedCode failed: %v", err)
    }
    if cid != "QmCodeDir" || source != "package main\n" {
        t.Fatalf("expected QmCodeDir and its main.go, got %q and %q", cid, source)
    }
}


func TestSharedCode_UnknownID_ReturnsErrShareNotFound(t *testing.T) {
    client := fakeKubo(t, map[string]string{}, nil)

    _, _, err := client.SharedCode(context.Background(), "abcdefgh")
    if !errors.Is(err, ErrShareNotFound) {
        t.Fatalf("expected ErrShareNotFound, got %v", err)
    }
}


func TestProvide_PropagatesTraceAndWaitsForKubo(t *testing.T) {
    prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
    otel.SetTracerProvider(sdktrace.NewTracerProvider())
    otel.SetTextMapPropagator(propagation.TraceContext{})
    t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

    var traceparent, providedCID string
    kubo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/v0/routing/provide" {
            t.Errorf("unexpected Kubo call %s", r.URL.Path)
        }
        traceparent = r.Header.Get("traceparent")
        providedCID = r.URL.Query().Get("arg")
        w.Header().Set("Content-Type", "application/json")
        w.(http.Flusher).Flush()
        time.Sleep(50 * time.Millisecond)
        _, _ = io.WriteString(w, `{"Type":2}`+"\n")
    }))
    defer kubo.Close()
    client, err := NewClient(kubo.URL)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }

    ctx, span := otel.Tracer("test").Start(context.Background(), "ipfs_provide")
    started := time.Now()
    err = client.Provide(ctx, "QmCodeDir")
    span.End()

    if err != nil {
        t.Fatalf("Provide failed: %v", err)
    }
    if time.Since(started) < 50*time.Millisecond {
        t.Fatal("Provide returned before Kubo finished announcing")
    }
    if providedCID != "QmCodeDir" {
        t.Fatalf("expected routing/provide for QmCodeDir, got %q", providedCID)
    }
    if !strings.Contains(traceparent, span.SpanContext().TraceID().String()) {
        t.Fatalf("expected a traceparent carrying trace %s, got %q", span.SpanContext().TraceID(), traceparent)
    }
}


func TestProvide_KuboError_ReturnsError(t *testing.T) {
    kubo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusInternalServerError)
        _, _ = io.WriteString(w, `{"Message":"cannot provide, no connected peers","Code":0,"Type":"error"}`)
    }))
    defer kubo.Close()
    client, err := NewClient(kubo.URL)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }

    if err := client.Provide(context.Background(), "QmCodeDir"); err == nil || !strings.Contains(err.Error(), "no connected peers") {
        t.Fatalf("expected Kubo's error, got %v", err)
    }
}
