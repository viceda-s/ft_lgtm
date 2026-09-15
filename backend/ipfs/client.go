package ipfs

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"

    "github.com/ipfs/boxo/files"
    rpc "github.com/ipfs/kubo/client/rpc"
)


// Client uploads execution artifacts to a Kubo IPFS node's RPC API.
type Client struct {
    api *rpc.HttpApi
}


// NewClient constructs a Client pointed at the Kubo RPC API reachable at apiURL (e.g. "http://kubo:5001" in-cluster, "http://127.0.0.1:5001" in dev).
func NewClient(apiURL string) (*Client, error) {
    api, err := rpc.NewURLApiWithClient(apiURL, &http.Client{})
    if err != nil {
        return nil, fmt.Errorf("contructing kubo rpc client: %w", err)
    }
    return &Client{api: api}, nil
}


// addEvent mirrors one line of Kubo's newline-delimited JSON response to POST /api/v0/add. 
// Kubo's add response has no field indicating node type; the wrapping directory (present because Upload always requests wrap-with-directory) is identified by having an empty Name, since Kubo wraps the upload in an unnamed files.FileEntry server-side.
type addEvent struct {
    Name string
    Hash string
}


// Upload packages source, stdout, and stderr into a directory (main.go, stdout.txt, stderr.txt), uploads it to Kubo with wrap-with-directory and pin enabled, and returns the wrapping directory's CID.
// The high-level Unixfs().Add() API has no wrap-with-directory option (it's a server-side-only flag), so this uses the low-level Request("add") builder directly.
func (c *Client) Upload(ctx context.Context, source, stdout, stderr string) (string, error) {
        dirNode := files.NewMapDirectory(map[string]files.Node{
                "main.go": files.NewBytesFile([]byte(source)),
                "stdout.txt": files.NewBytesFile([]byte(stdout)),
                "stderr.txt": files.NewBytesFile([]byte(stderr)),
        })
        // wrapped := files.NewMapDirectory(map[string]files.Node{"": dirNode})
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