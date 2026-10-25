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


type Client struct {
    api *rpc.HttpApi
}


func NewClient(apiURL string) (*Client, error) {
    api, err := rpc.NewURLApiWithClient(apiURL, &http.Client{})
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