// Package joinpeer is the joiner's side of a share for keel-native: it reads a join link, checks the host's WebRTC peer
// against the key in the link, and gives the window a connection that reads the host's terminal and writes what the
// window allows. The link, the MAC and the signal URL are internal/sharelink's, the same code keel serve's share hub uses.
//
// The link's key (the #k= fragment) is a secret: it is used for the check of the host's MAC and for nothing else. It is
// never sent to the signal server, never logged, never put in an error, and the types that hold it print without it.
package joinpeer

import (
	"net/url"

	"keel/internal/sharelink"
	"keel/internal/wire"
)

// Link is a parsed join link (sharelink.ParseLink reads one).
type Link = sharelink.Link

// signalURL is the signal server's websocket for a joiner. It carries the room and the name and nothing of the key.
func signalURL(origin, room, name string) string {
	if name == "" {
		name = "guest"
	}
	return sharelink.SocketURL(origin, url.Values{"role": {wire.SignalRoleJoin}, "room": {room}, "name": {name}})
}
