package sharelink

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Link is a parsed join link. Key is the share's secret: String, GoString and Format leave it out.
type Link struct {
	Origin string // scheme://host[:port] of the signal server
	Room   string
	Key    string
}

func (l Link) String() string   { return fmt.Sprintf("{%s room=%s}", l.Origin, l.Room) }
func (l Link) GoString() string { return "sharelink.Link" + l.String() }

// JoinURL is the link the host hands out: the signal server, the room and the key after "#", which no
// request carries.
func JoinURL(signal, room, key string) string {
	return signal + "/join?room=" + url.QueryEscape(room) + "#k=" + key
}

var keyRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ParseLink reads a /join?room=…#k=… link. The #k secret authenticates the host's peer, so a link without one is
// refused rather than joined unverified. The errors are the app's words. Only an http or https link is read (the signal
// server is dialled over ws or wss).
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
