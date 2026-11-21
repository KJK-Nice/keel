package slimeclient

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"slime/internal/authtoken"
)

// ReadSessionFile copies a file the user ⌘-clicked in a remote pane (name as printed) from the session's host:
// GET /api/sessions/{id}/file. It returns the file and the path slime serve read. ctx bounds it: a big file
// takes longer than the 15 s of the other calls.
func (c *Client) ReadSessionFile(ctx context.Context, sessionID, name string) ([]byte, string, error) {
	p := "/api/sessions/" + url.PathEscape(sessionID) + "/file"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.addr+p+"?path="+url.QueryEscape(name), nil)
	if err != nil {
		return nil, "", err
	}
	authtoken.SetToken(req.Header, c.token)
	resp, err := c.slow.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", statusError(http.MethodGet, p, resp)
	}
	data, err := io.ReadAll(resp.Body)
	return data, resp.Header.Get("X-Slime-Path"), err
}
