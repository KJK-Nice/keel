package sharelink

import (
	"net/url"
	"strings"
)

// STUN is the one ICE server a share uses, on both sides.
const STUN = "stun:stun.l.google.com:19302"

// SocketURL is the signal server's websocket (/ws) on the server at base (its http or https URL), with the query
// parameters q (role, room, name, the host's key). https becomes wss, anything else ws.
func SocketURL(base string, q url.Values) string {
	u, err := url.Parse(base)
	if err != nil {
		return strings.Replace(base, "http", "ws", 1) + "/ws?" + q.Encode()
	}
	u.Scheme = map[string]string{"https": "wss"}[u.Scheme]
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	u.RawQuery = q.Encode()
	return u.String()
}
