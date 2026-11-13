package keelclient

import (
	"context"
	"net/http"
	"net/url"
)

// Port forwarding: tunnel.Info as GET /api/tunnels lists it. Each forward opens its own SSH connection from keel
// serve and keeps running when the window closes.

// Tunnel is a forward and its state. Kind is local (-L), remote (-R) or dynamic (-D, SOCKS5); Status is stopped,
// connecting, ready or error; ListenAddr is the address it really listens on (a bind port 0 picks one).
type Tunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	HostID     string `json:"hostId"`
	Kind       string `json:"kind"`
	BindAddr   string `json:"bindAddr"`
	BindPort   int    `json:"bindPort"`
	DestHost   string `json:"destHost"`
	DestPort   int    `json:"destPort"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	ListenAddr string `json:"listenAddr"`
}

// Tunnels is GET /api/tunnels.
func (c *Client) Tunnels(ctx context.Context) (out []Tunnel, err error) {
	err = c.do(ctx, http.MethodGet, "/api/tunnels", nil, &out)
	return out, err
}

// TunnelInput is what a save sends.
type TunnelInput struct {
	ID       string `json:"-"` // "" creates, else updates
	Name     string `json:"name"`
	HostID   string `json:"hostId"`
	Kind     string `json:"kind"`
	BindAddr string `json:"bindAddr"`
	BindPort int    `json:"bindPort"`
	DestHost string `json:"destHost,omitempty"`
	DestPort int    `json:"destPort,omitempty"`
}

// SaveTunnel creates a forward (POST /api/tunnels) or updates one (PUT /api/tunnels/{id}). It does not start it.
func (c *Client) SaveTunnel(ctx context.Context, in TunnelInput) (out Tunnel, err error) {
	if in.ID == "" {
		err = c.do(ctx, http.MethodPost, "/api/tunnels", in, &out)
	} else {
		err = c.do(ctx, http.MethodPut, "/api/tunnels/"+url.PathEscape(in.ID), in, &out)
	}
	return out, err
}

// DeleteTunnel is DELETE /api/tunnels/{id}; the server stops the forward first.
func (c *Client) DeleteTunnel(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/tunnels/"+url.PathEscape(id), nil, nil)
}

// StartTunnel is POST /api/tunnels/{id}/start. A host whose key is unknown answers with a *StatusError whose Offer
// is the key to trust.
func (c *Client) StartTunnel(ctx context.Context, id string) (out Tunnel, err error) {
	err = c.do(ctx, http.MethodPost, "/api/tunnels/"+url.PathEscape(id)+"/start", nil, &out)
	return out, err
}

// StopTunnel is POST /api/tunnels/{id}/stop.
func (c *Client) StopTunnel(ctx context.Context, id string) (out Tunnel, err error) {
	err = c.do(ctx, http.MethodPost, "/api/tunnels/"+url.PathEscape(id)+"/stop", nil, &out)
	return out, err
}
