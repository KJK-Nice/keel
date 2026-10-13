package signal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRoomRelay(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rooms %d", res.StatusCode)
	}
	var body struct {
		ID      string `json:"id"`
		HostKey string `json:"hostKey"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == "" {
		t.Fatalf("room id: %v %#v", err, body)
	}

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	host, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=host&room="+body.ID+"&key="+body.HostKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	join, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=join&room="+body.ID+"&name=bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer join.Close()

	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	var peer map[string]any
	if err := host.ReadJSON(&peer); err != nil {
		t.Fatal(err)
	}
	if peer["type"] != "peer" {
		t.Fatalf("host first = %#v", peer)
	}
	peerID, _ := peer["peerId"].(string)
	if err := host.WriteJSON(map[string]any{"type": "signal", "peerId": peerID, "kind": "offer", "sdp": "v=0", "mac": "abc"}); err != nil {
		t.Fatal(err)
	}
	_ = join.SetReadDeadline(time.Now().Add(2 * time.Second))
	var toJoin map[string]any
	if err := join.ReadJSON(&toJoin); err != nil {
		t.Fatal(err)
	}
	if toJoin["kind"] != "offer" || toJoin["sdp"] != "v=0" || toJoin["mac"] != "abc" {
		t.Fatalf("join got %#v (the relay must keep mac)", toJoin)
	}
	if err := join.WriteJSON(map[string]any{"type": "signal", "kind": "answer", "sdp": "v=1", "mac": "def"}); err != nil {
		t.Fatal(err)
	}
	var toHost map[string]any
	if err := host.ReadJSON(&toHost); err != nil {
		t.Fatal(err)
	}
	if toHost["kind"] != "answer" || toHost["mac"] != "def" || toHost["peerId"] != peerID {
		t.Fatalf("host got %#v (the relay must keep mac)", toHost)
	}
}

func TestJoinPage(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/join?room=x")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	html := string(body)
	if strings.Contains(html, "jsdelivr") || strings.Contains(html, "cdn.") {
		t.Fatalf("join page still loads a CDN: %s", html)
	}
	if !strings.Contains(html, "/vendor/xterm/xterm.min.js") {
		t.Fatal("expected vendored xterm")
	}
	if !strings.Contains(html, "addon-unicode-graphemes.js") ||
		!strings.Contains(html, "addon-web-links.js") ||
		!strings.Contains(html, "addon-progress.js") {
		t.Fatal("join page must load unicode graphemes, web-links, and progress addons")
	}
	if strings.Contains(html, "addon-search.js") {
		t.Fatal("join page should not load the search addon")
	}
	css, err := http.Get(srv.URL + "/vendor/xterm/xterm.css")
	if err != nil {
		t.Fatal(err)
	}
	defer css.Body.Close()
	if css.StatusCode != http.StatusOK {
		t.Fatalf("xterm.css %d", css.StatusCode)
	}
	js, err := http.Get(srv.URL + "/vendor/xterm/xterm.min.js")
	if err != nil {
		t.Fatal(err)
	}
	defer js.Body.Close()
	if js.StatusCode != http.StatusOK {
		t.Fatalf("xterm.min.js %d", js.StatusCode)
	}
	jsBody, _ := io.ReadAll(js.Body)
	cssBody, _ := io.ReadAll(css.Body)
	if !strings.Contains(string(jsBody), "@xterm/xterm@6.0.0") || !strings.Contains(string(cssBody), "@xterm/xterm@6.0.0") {
		t.Fatal("join page must vendor xterm 6.0.0")
	}
	for _, spec := range []struct {
		path   string
		banner string
		global string
	}{
		{"/vendor/xterm/addon-unicode-graphemes.js", "@xterm/addon-unicode-graphemes@0.4.0", "window.UnicodeGraphemesAddon=i"},
		{"/vendor/xterm/addon-web-links.js", "@xterm/addon-web-links@0.12.0", "window.WebLinksAddon=i"},
		{"/vendor/xterm/addon-progress.js", "@xterm/addon-progress@0.2.0", "window.ProgressAddon=i"},
	} {
		res, err := http.Get(srv.URL + spec.path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s %d", spec.path, res.StatusCode)
		}
		if !strings.Contains(string(b), spec.banner) || !bytes.Contains(b, []byte(spec.global)) {
			t.Fatalf("join %s must vendor %s onto window", spec.path, spec.banner)
		}
	}
	joinJS, err := http.Get(srv.URL + "/join.js")
	if err != nil {
		t.Fatal(err)
	}
	defer joinJS.Body.Close()
	joinBody, _ := io.ReadAll(joinJS.Body)
	if !strings.Contains(string(joinBody), "UnicodeGraphemesAddon") ||
		!strings.Contains(string(joinBody), "WebLinksAddon") ||
		!strings.Contains(string(joinBody), "ProgressAddon") ||
		!strings.Contains(string(joinBody), "openJoinLink") {
		t.Fatal("join.js must wire unicode graphemes, web links, and progress")
	}
	if !strings.Contains(string(joinBody), `from "/joinpeer.js"`) {
		t.Fatal("join.js must connect through /joinpeer.js (which verifies the host)")
	}
	peerJS, err := http.Get(srv.URL + "/joinpeer.js")
	if err != nil {
		t.Fatal(err)
	}
	peerBody, _ := io.ReadAll(peerJS.Body)
	peerJS.Body.Close()
	if peerJS.StatusCode != http.StatusOK || !strings.Contains(string(peerBody), `from "./sharemac.js"`) {
		t.Fatalf("/joinpeer.js = %d, must import ./sharemac.js", peerJS.StatusCode)
	}
	page, err := http.Get(srv.URL + "/join")
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(pageBody), `<script type="module" src="/join.js">`) {
		t.Fatal("join.html must load join.js as a module (it imports sharemac.js)")
	}
	mac, err := http.Get(srv.URL + "/sharemac.js")
	if err != nil {
		t.Fatal(err)
	}
	macBody, _ := io.ReadAll(mac.Body)
	mac.Body.Close()
	if mac.StatusCode != http.StatusOK || !strings.HasPrefix(mac.Header.Get("Content-Type"), "text/javascript") ||
		!strings.Contains(string(macBody), "export async function answerOffer") {
		t.Fatalf("/sharemac.js = %d %q", mac.StatusCode, mac.Header.Get("Content-Type"))
	}
	fsMark := bytes.Index(jsBody, []byte(`e.FS="`))
	usMark := bytes.Index(jsBody, []byte(`e.US=`))
	if fsMark < 0 || usMark <= fsMark || bytes.Contains(jsBody[fsMark:usMark], []byte("\n")) ||
		!bytes.Contains(jsBody[fsMark:usMark], []byte{0x1c}) {
		t.Fatal("join xterm C0 File/Group/Record separators must stay inside the minified string literals")
	}
}

func TestRoomCapAndExpiry(t *testing.T) {
	s := New()
	s.maxRooms = 2
	s.roomTTL = 200 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	mustRoom := func() (string, string) {
		t.Helper()
		res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("rooms %d", res.StatusCode)
		}
		var body struct {
			ID      string `json:"id"`
			HostKey string `json:"hostKey"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == "" {
			t.Fatalf("room id: %v %#v", err, body)
		}
		return body.ID, body.HostKey
	}
	mustRoom()
	mustRoom()
	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("cap status %d", res.StatusCode)
	}

	time.Sleep(250 * time.Millisecond)
	id, key := mustRoom()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(ws+"/ws?role=host&room="+id+"&key="+key, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestSignalRejectsCrossSiteOrigin(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/rooms", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		ID      string `json:"id"`
		HostKey string `json:"hostKey"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?role=host&room=" + body.ID + "&key=" + body.HostKey
	hdr := http.Header{}
	hdr.Set("Origin", "https://evil.example")
	conn, resp, err := websocket.DefaultDialer.Dial(ws, hdr)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-site websocket reject")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ws status = %d", resp.StatusCode)
	}
	hdr.Set("Origin", srv.URL)
	conn, _, err = websocket.DefaultDialer.Dial(ws, hdr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}
