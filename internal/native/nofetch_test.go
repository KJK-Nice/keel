package native

import (
	"strings"
	"testing"
)

// A markdown image in a notebook or an agent's answer must not make the window fetch a URL its author chose
// (docs/ui-redesign.md §18). The native window holds that property by having no HTTP client of its own: the
// window's code (this package) shows model and notebook text as text and imports net/http nowhere; the only requests
// go through keelclient to keel serve, and the share join through joinpeer.
func TestTheWindowCodeHasNoHTTPClientSoNoTextCanMakeItFetchAURL(t *testing.T) {
	for name, src := range sourceFiles(t) {
		if strings.Contains(name, "/") { // keelclient/, joinpeer/, sshparse/, layout/: their own packages
			continue
		}
		if strings.Contains(src, `"net/http"`) {
			t.Errorf("%s imports net/http: the window code must not fetch URLs; requests go through keelclient", name)
		}
	}
}
