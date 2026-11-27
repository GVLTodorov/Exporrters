package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxBodyBytes caps how much of one API response is decoded. The largest
// replies are Radarr's movie list and Sonarr's series list (roughly 5 KB per
// movie or series), so this fits libraries of well over 10,000 items while
// stopping a misbehaving proxy from exhausting memory. A variable so the
// leak tests can lower it.
var maxBodyBytes int64 = 64 << 20

// drainBytes is how much of an unread body is discarded before closing it,
// so the keep-alive connection can be reused instead of re-dialled.
const drainBytes = 64 << 10

// apiClient talks to one application's API. Every request carries the API
// key in the X-Api-Key header (accepted by Sonarr, Radarr, Prowlarr and
// Bazarr), so the key never appears in URLs, logs or error messages.
type apiClient struct {
	http   *http.Client
	apiURL string // base URL + API path, e.g. http://host:8989/api/v3
	apiKey string
}

// get fetches apiURL+path?query and decodes the JSON reply into out.
func (c *apiClient) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.apiURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s", path, resp.Status)
	}
	lr := &io.LimitedReader{R: resp.Body, N: maxBodyBytes}
	if err := json.NewDecoder(lr).Decode(out); err != nil {
		if lr.N == 0 {
			return fmt.Errorf("%s: response larger than %d bytes", path, maxBodyBytes)
		}
		return fmt.Errorf("%s: decoding response: %w", path, err)
	}
	return nil
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, drainBytes))
	_ = body.Close()
}
