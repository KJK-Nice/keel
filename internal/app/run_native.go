package app

import (
	"io/fs"
	"log"

	"keel/internal/buildid"
	"keel/internal/native"
)

// RunNative is keel app's default window: the MyGo client in this process, on the keel serve that PrepareServe
// started or found. No Keel.app re-exec (EnsureMacApp): the native window needs no signed bundle, that is the
// webview's WKWebView. web only feeds the build id, which keel serve reports the same way (internal/buildid).
func RunNative(web fs.FS, addr, dataDir string) error {
	if _, err := PrepareServe(addr, dataDir); err != nil {
		return err
	}
	log.Printf("Keel app on http://%s — close the window to detach; sessions stay in keel serve", addr)
	return native.RunWith(native.Options{Addr: addr, DataDir: dataDir, AppBuild: buildid.Of(web)})
}
