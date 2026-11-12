package app

import (
	"log"

	"keel/internal/buildid"
	"keel/internal/native"
)

// RunNative is keel app's window: the MyGo client in this process, on the keel serve that PrepareServe started or
// found. It needs no signed bundle. Its build id is the one keel serve reports the same way (internal/buildid).
func RunNative(addr, dataDir string) error {
	if _, err := PrepareServe(addr, dataDir); err != nil {
		return err
	}
	log.Printf("Keel app on http://%s — close the window to detach; sessions stay in keel serve", addr)
	return native.RunWith(native.Options{Addr: addr, DataDir: dataDir, AppBuild: buildid.Of()})
}
