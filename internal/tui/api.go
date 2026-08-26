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
	Token  string
	Name   string
	client *http.Client
}

func NewAPI(base, token, name string) *API {
	return &API{
		Base:   strings.TrimRight(base, "/"),
		Token:  token,
		Name:   name,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

func (a *API) applyAuth(req *http.Request) {
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
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
	a.applyAuth(req)
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
	You    runtime.Client
	Events chan runtime.Event
}

func (a *API) wsURL(path string, query url.Values) (*url.URL, error) {
	u, err := url.Parse(a.Base)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = path
	if a.Token != "" {
		query.Set("token", a.Token)
	}
	u.RawQuery = query.Encode()
	return u, nil
}

func (a *API) Attach(id string, cols, rows int) (*Conn, error) {
	q := url.Values{}
	q.Set("id", id)
	q.Set("cols", fmt.Sprintf("%d", cols))
	q.Set("rows", fmt.Sprintf("%d", rows))
	q.Set("name", a.Name)
	q.Set("kind", "tui")
	u, err := a.wsURL("/ws/session", q)
	if err != nil {
		return nil, err
	}
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	c := &Conn{ws: ws, Events: make(chan runtime.Event, 128)}
	go c.readLoop()
	return c, nil
}

func (a *API) Herd() (*HerdConn, error) {
	u, err := a.wsURL("/ws/herd", url.Values{})
	if err != nil {
		return nil, err
	}
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	c := &HerdConn{ws: ws, Events: make(chan HerdEvent, 32)}
	go c.readLoop()
	return c, nil
}

type HerdConn struct {
	ws     *websocket.Conn
	Events chan HerdEvent
}

type HerdEvent struct {
	Type     string
	Sessions []runtime.Info
	Session  runtime.Info
	ID       string
	Closed   bool
}

func (c *HerdConn) Close() {
	_ = c.ws.Close()
}

func (c *HerdConn) readLoop() {
	defer close(c.Events)
	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			c.Events <- HerdEvent{Closed: true}
			return
		}
		var body struct {
			Type     string         `json:"type"`
			Sessions []runtime.Info `json:"sessions"`
			Session  runtime.Info   `json:"session"`
			ID       string         `json:"id"`
		}
		if json.Unmarshal(msg, &body) != nil {
			continue
		}
		c.Events <- HerdEvent{Type: body.Type, Sessions: body.Sessions, Session: body.Session, ID: body.ID}
	}
}

func (c *Conn) Write(p []byte) error {
	return c.ws.WriteMessage(websocket.BinaryMessage, p)
}

func (c *Conn) Resize(cols, rows int) error {
	return c.ws.WriteJSON(map[string]any{"type": "resize", "cols": cols, "rows": rows})
}

func (c *Conn) RequestControl() error {
	return c.ws.WriteJSON(map[string]any{"type": "request-control"})
}

func (c *Conn) TakeControl() error {
	return c.ws.WriteJSON(map[string]any{"type": "take-control"})
}

func (c *Conn) GrantControl(target uint64) error {
	return c.ws.WriteJSON(map[string]any{"type": "grant-control", "targetId": target})
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
		ev, you := parseSessionJSON(msg)
		if you.ID != 0 {
			c.You = you
		}
		c.Events <- ev
	}
}

func parseSessionJSON(msg []byte) (runtime.Event, runtime.Client) {
	var body struct {
		Type     string           `json:"type"`
		Message  string           `json:"message"`
		Status   string           `json:"status"`
		Attached []runtime.Client `json:"attached"`
		DriverID uint64           `json:"driverId"`
		OwnerID  uint64           `json:"ownerId"`
		FromID   uint64           `json:"fromId"`
		TargetID uint64           `json:"targetId"`
		You      runtime.Client   `json:"you"`
		ID       string           `json:"id"`
		Title    string           `json:"title"`
	}
	if json.Unmarshal(msg, &body) != nil {
		return runtime.Event{}, runtime.Client{}
	}
	st := runtime.Status(body.Status)
	if body.Type == "error" {
		st = runtime.StatusError
	}
	info := runtime.Info{
		ID:       body.ID,
		Title:    body.Title,
		Status:   st,
		Attached: body.Attached,
		DriverID: body.DriverID,
		OwnerID:  body.OwnerID,
		Clients:  len(body.Attached),
	}
	return runtime.Event{
		Kind:     body.Type,
		Status:   st,
		Message:  body.Message,
		Info:     info,
		FromID:   body.FromID,
		TargetID: body.TargetID,
	}, body.You
}

func (a *API) get(path string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, a.Base+path, nil)
	if err != nil {
		return err
	}
	a.applyAuth(req)
	res, err := a.client.Do(req)
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
	req, err := http.NewRequest(http.MethodPost, a.Base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyAuth(req)
	res, err := a.client.Do(req)
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
