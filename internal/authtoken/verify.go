package authtoken

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// Challenge names one of the proofs /api/health gives: the query parameter
// that carries the client's nonce and the JSON field that carries the
// server's answer. The server (httpapi) and the clients read the same two.
type Challenge struct{ Param, Field string }

var (
	// WindowChallenge proves the token of Header (the window, keel serve stop).
	WindowChallenge = Challenge{Param: "nonce", Field: "proof"}
	// MCPChallenge proves the token of MCPHeader (keel mcp).
	MCPChallenge = Challenge{Param: "mcpNonce", Field: "mcpProof"}
)

// ErrUnproven is VerifyServer's answer when a server replied without
// proving it holds the token.
var ErrUnproven = errors.New("the server did not prove it holds the token")

// NoRedirect is the CheckRedirect of every HTTP client that sends a token
// header: a redirect would carry the header to wherever it points.
func NoRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// VerifyServer checks that the server on addr holds token before a client
// hands the token to it. It sends a random nonce, never the token, and
// expects HMAC(token, nonce) back: another program that took the port
// cannot answer. A server that cannot be reached is the transport's error;
// one that answers without the proof is ErrUnproven.
func VerifyServer(ctx context.Context, c *http.Client, addr string, ch Challenge, token string) error {
	nonce, err := NewNonce()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/health?"+ch.Param+"="+url.QueryEscape(nonce), nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var body map[string]json.RawMessage
	if res.StatusCode == http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	}
	var proof string
	_ = json.Unmarshal(body[ch.Field], &proof)
	if !CheckProof(token, nonce, proof) {
		return ErrUnproven
	}
	return nil
}
