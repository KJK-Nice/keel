package app

import (
	"log"

	"keel/internal/buildid"
	"keel/internal/native"
)

// RunNative is slime app's window: the MyGo client in this process, on the slime serve that PrepareServe started or
// found. It needs no signed bundle. Its build id is the one slime serve reports the same way (internal/buildid).
// name is the display name for sessions this window shares and joins.
func RunNative(addr, dataDir, name string) error {
	if _, err := PrepareServe(addr, dataDir); err != nil {
		return err
	}
	log.Printf("Slime app on http://%s — close the window to detach; sessions stay in slime serve", addr)
	return native.RunWith(native.Options{Addr: addr, DataDir: dataDir, AppBuild: buildid.Of(), Name: name})
}
