package ipfs

import (
    "context"
    "crypto/sha256"
    "encoding/base32"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"

    "github.com/ipfs/boxo/files"
    rpc "github.com/ipfs/kubo/client/rpc"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)


type Client struct {
    api *rpc.HttpApi
}


func NewClient(apiURL string) (*Client, error) {
    transport := otelhttp.NewTransport(http.DefaultTransport,
        otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
            return "kubo " + strings.TrimPrefix(r.URL.Path, "/api/v0/")
        }))
    api, err := rpc.NewURLApiWithClient(apiURL, &http.Client{Transport: transport})
    if err != nil {
        return nil, fmt.Errorf("contructing kubo rpc client: %w", err)
    }
    return &Client{api: api}, nil
}


type addEvent struct {
    Name string
    Hash string
}


func (c *Client) Upload(ctx context.Context, source, stdout, stderr string) (string, error) {
        dirNode := files.NewMapDirectory(map[string]files.Node{
                "main.go": files.NewBytesFile([]byte(source)),
                "stdout.txt": files.NewBytesFile([]byte(stdout)),
                "stderr.txt": files.NewBytesFile([]byte(stderr)),
        })
        reader := files.NewMultiFileReader(dirNode, false, false)

        resp, err := c.api.Request("add").
                Option("recursive", true).
                Option("wrap-with-directory", true).
                Option("pin", true).
                Body(reader).
                Send(ctx)
        if err != nil {
                return "", fmt.Errorf("sending add request: %w", err)
        }
        defer resp.Close()

        if resp.Error != nil {
            return "", fmt.Errorf("kubo add failed: %w", resp.Error)
        }

        dec := json.NewDecoder(resp.Output)
        var wrapperCID string
        for {
            var evt addEvent
            if err := dec.Decode(&evt); err != nil {
                if err == io.EOF {
                    break
                }
                return "", fmt.Errorf("decoding add response: %w", err)
            }
            if evt.Name == "" {
                if wrapperCID != "" {
                    return "", fmt.Errorf("kubo add response contains more than one unnamed entry, cannot determine wrapper CID")
                }
                if evt.Hash == "" {
                    return "", fmt.Errorf("kubo add response contains an unnamed entry with no hash")
                }
                wrapperCID = evt.Hash
            }
        }

        if wrapperCID == "" {
            return "", fmt.Errorf("kubo add response had no wrapping directory entry") 
        }
        return wrapperCID, nil
}


var ErrShareNotFound = errors.New("share not found")

const sharesDir = "/shares"

const maxSharedSourceBytes = 64 * 1024


func shareID(cid string, length int) string {
    sum := sha256.Sum256([]byte(cid))
    encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
    return strings.ToLower(encoded[:length])
}


func (c *Client) Share(ctx context.Context, cid string) (string, error) {
    for _, length := range []int{8, 16} {
        id := shareID(cid, length)
        cpErr := c.api.Request("files/cp", "/ipfs/"+cid, sharesDir+"/"+id).
            Option("parents", true).
            Exec(ctx, nil)
        if cpErr == nil {
            return id, nil
        }
        existing, err := c.sharedCID(ctx, id)
        if err != nil {
            return "", fmt.Errorf("creating share link: %w", cpErr)
        }
        if existing == cid {
            return id, nil
        }
    }
    return "", fmt.Errorf("every share id for %s is taken by other content", cid)
}


func (c *Client) SharedCode(ctx context.Context, id string) (string, string, error) {
    cid, err := c.sharedCID(ctx, id)
    if err != nil {
        return "", "", err
    }

    resp, err := c.api.Request("cat", "/ipfs/"+cid+"/main.go").Send(ctx)
    if err != nil {
        return "", "", fmt.Errorf("sending cat request: %w", err)
    }
    defer resp.Close()
    if resp.Error != nil {
        return "", "", fmt.Errorf("kubo cat failed: %w", resp.Error)
    }

    source, err := io.ReadAll(io.LimitReader(resp.Output, maxSharedSourceBytes))
    if err != nil {
        return "", "", fmt.Errorf("reading shared source: %w", err)
    }
    return cid, string(source), nil
}


func (c *Client) sharedCID(ctx context.Context, id string) (string, error) {
    var stat struct{ Hash string }
    if err := c.api.Request("files/stat", sharesDir+"/"+id).Exec(ctx, &stat); err != nil {
        if strings.Contains(err.Error(), "file does not exist") {
            return "", ErrShareNotFound
        }
        return "", fmt.Errorf("kubo files stat failed: %w", err)
    }
    return stat.Hash, nil
}


func (c *Client) Provide(ctx context.Context, cid string) error {
    resp, err := c.api.Request("routing/provide", cid).Send(ctx)
    if err != nil {
        return fmt.Errorf("sending provide request: %w", err)
    }
    defer resp.Close()
    if resp.Error != nil {
        return fmt.Errorf("kubo provide failed: %w", resp.Error)
    }
    if _, err := io.Copy(io.Discard, resp.Output); err != nil {
        return fmt.Errorf("waiting for provide to finish: %w", err)
    }
    return nil
}
