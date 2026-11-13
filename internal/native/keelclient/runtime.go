package keelclient

import (
	"context"
	"net/http"
)

// Runtime is GET /api/runtime (Settings · Runtime, Sharing and About): keel serve's own status. It holds no secret.
// SignalSource is env, settings, build or "": where the signal server in use (SignalURL) comes from; SignalSaved is the
// one saved in Settings, which KEEL_SIGNAL overrides for the run.
type Runtime struct {
	Build        string `json:"build"`
	PID          int    `json:"pid"`
	Addr         string `json:"addr"`
	DataDir      string `json:"dataDir"`
	Sessions     int    `json:"sessions"`
	UptimeSec    int64  `json:"uptimeSec"`
	CanStop      *bool  `json:"canStop"` // nil reads as true (settingspages.js: canStop === false disables Stop)
	SignalURL    string `json:"signalURL"`
	SignalSource string `json:"signalSource"`
	SignalSaved  string `json:"signalSaved"`
}

// Runtime is GET /api/runtime.
func (c *Client) Runtime(ctx context.Context) (out Runtime, err error) {
	err = c.do(ctx, http.MethodGet, "/api/runtime", nil, &out)
	return out, err
}

// SetSignal is PUT /api/share/signal: it saves the signal server ("" removes it). keel serve validates the address and
// answers with the signal fields of Runtime.
func (c *Client) SetSignal(ctx context.Context, url string) (out Runtime, err error) {
	err = c.do(ctx, http.MethodPut, "/api/share/signal", map[string]string{"url": url}, &out)
	return out, err
}

// StopRuntime is POST /api/runtime/stop: keel serve ends every session and exits. It answers 202 and stops after.
func (c *Client) StopRuntime(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/runtime/stop", nil, nil)
}
