package threat

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// MaxBody begrenzt die Größe einer Liste (blocklist.de ist ~1 MB).
const MaxBody = 8 << 20

// Fetch lädt eine Liste. URL-Basis ist in Tests über Feed.URL austauschbar.
func Fetch(ctx context.Context, client *http.Client, f Feed) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "roster-rmm (threat feeds)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", f.Name, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%s: leere Antwort", f.Name)
	}
	return body, nil
}
