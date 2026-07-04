package transport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DownloadVNCBundle lädt das native VNC-Server-Bundle (ZIP) einer Plattform vom
// Server. Nutzt den Stream-Client (ohne kurzes Timeout), da das Bundle mehrere MB
// groß sein kann. Liefert zusätzlich den vom Server gemeldeten SHA-256.
func (c *Client) DownloadVNCBundle(ctx context.Context, agentToken, platform string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/agent/vnc/"+url.PathEscape(platform), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("vnc-bundle: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // max 64 MB
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("X-VNC-SHA256"), nil
}
