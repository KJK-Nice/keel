package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/runtime"
	"keel/internal/store"
)

type API struct {
	Base   string
	client *http.Client
}

func NewAPI(base string) *API {
	return &API{
		Base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

func (a *API) Hosts() ([]store.Host, error) {
	var hosts []store.Host
	if err := a.get("/api/hosts", &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

func (a *API) Snippets() ([]store.Snippet, error) {
	var sn []store.Snippet
	if err := a.get("/api/snippets", &sn); err != nil {
		return nil, err
	}
	return sn, nil
}

func (a *API) Sessions() ([]runtime.Info, error) {
	var list []runtime.Info
	if err := a.get("/api/sessions", &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (a *API) Spawn(hostID string, cols, rows int) (runtime.Info, error) {
	var info runtime.Info
	err := a.post("/api/sessions", map[string]any{
		"hostId": hostID,
		"cols":   cols,
		"rows":   rows,
	}, &info)
	return info, err
}

func (a *API) Kill(id string) error {
	req, err := http.NewRequest(http.MethodDelete, a.Base+"/api/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("kill: %s", strings.TrimSpace(string(b)))
	}
	return nil
}

type Conn struct {
	ws     *websocket.Conn
	Events chan runtime.Event
}

func (a *API) Attach(id string, cols, rows int) (*Conn, error) {
	u, err := url.Parse(a.Base)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/session"
	q := u.Query()
	q.Set("id", id)
	q.Set("cols", fmt.Sprintf("%d", cols))
	q.Set("rows", fmt.Sprintf("%d", rows))
	u.RawQuery = q.Encode()

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	c := &Conn{ws: ws, Events: make(chan runtime.Event, 128)}
	go c.readLoop()
	return c, nil
}

func (c *Conn) Write(p []byte) error {
	return c.ws.WriteMessage(websocket.BinaryMessage, p)
}

func (c *Conn) Resize(cols, rows int) error {
	return c.ws.WriteJSON(map[string]any{"type": "resize", "cols": cols, "rows": rows})
}

func (c *Conn) Close() {
	_ = c.ws.Close()
}

func (c *Conn) readLoop() {
	defer close(c.Events)
	for {
		mt, msg, err := c.ws.ReadMessage()
		if err != nil {
			c.Events <- runtime.Event{Kind: "status", Status: runtime.StatusExited, Message: "detached"}
			return
		}
		if mt == websocket.BinaryMessage {
			c.Events <- runtime.Event{Kind: "bytes", Bytes: append([]byte(nil), msg...)}
			continue
		}
		var body struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Status  string `json:"status"`
		}
		if json.Unmarshal(msg, &body) != nil {
			continue
		}
		st := runtime.Status(body.Status)
		if body.Type == "error" {
			st = runtime.StatusError
		}
		c.Events <- runtime.Event{Kind: "status", Status: st, Message: body.Message}
	}
}

func (a *API) get(path string, dest any) error {
	res, err := a.client.Get(a.Base + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %s", path, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

func (a *API) post(path string, payload, dest any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	res, err := a.client.Post(a.Base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %s", path, strings.TrimSpace(string(body)))
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dest)
}
