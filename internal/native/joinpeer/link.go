// Package joinpeer is the joiner's side of a share for keel-native: it reads a join link, checks the host's WebRTC peer
// against the key in the link, and gives the window a connection that reads the host's terminal and writes what the
// window allows. It ports cmd/keel/web/js/lib/joinlink.js, lib/sharemac.js and lib/joinpeer.js; the server's half of the
// check is internal/share/verify.go.
//
// The link's key (the #k= fragment) is a secret: it is used for the check of sharemac.js and for nothing else. It is
// never sent to the signal server, never logged, never put in an error, and the types that hold it print without it.
package joinpeer

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"keel/internal/wire"
)

// Link is a parsed join link. Key is the share's secret: String, GoString and Format leave it out.
type Link struct {
	Origin string // scheme://host[:port] of the signal server
	Room   string
	Key    string
}

func (l Link) String() string   { return fmt.Sprintf("{%s room=%s}", l.Origin, l.Room) }
func (l Link) GoString() string { return "joinpeer.Link" + l.String() }

var keyRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ParseLink reads a /join?room=…#k=… link (lib/joinlink.js parseJoinLink). The #k secret authenticates the host's
// peer, so a link without one is refused rather than joined unverified. The errors are the app's words. Beyond the
// app: only an http or https link is read (the signal server is dialled over ws or wss).
func ParseLink(input string) (Link, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Link{}, errors.New("paste a Share link")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Link{}, errors.New("paste the full Share link")
	}
	room := u.Query().Get("room")
	if room == "" {
		return Link{}, errors.New("link has no room")
	}
	key := ""
	for _, kv := range strings.Split(u.EscapedFragment(), "&") {
		k, v, _ := strings.Cut(kv, "=")
		if k, err = url.QueryUnescape(k); err == nil && k == "k" {
			key, _ = url.QueryUnescape(v)
			break
		}
	}
	if !keyRE.MatchString(key) {
		return Link{}, errors.New("link has no key (#k=…): ask the host for a new Share link")
	}
	return Link{Origin: u.Scheme + "://" + u.Host, Room: room, Key: key}, nil
}

// signalURL is the signal server's websocket for a joiner (joinpeer.js signalURL). It carries the room and the name
// and nothing of the key.
func signalURL(origin, room, name string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	u.Scheme = map[string]string{"https": "wss"}[u.Scheme]
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	if name == "" {
		name = "guest"
	}
	u.Path, u.Fragment = "/ws", ""
	u.RawQuery = url.Values{"role": {wire.SignalRoleJoin}, "room": {room}, "name": {name}}.Encode()
	return u.String()
}
