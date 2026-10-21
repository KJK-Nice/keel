package desktop

import (
	"net/url"
	"strings"
	"unicode"
)

// Links in agent answers (cmd/keel/web/js/lib/markdown.js) open in the
// system browser. The page has no Wails runtime and WKWebView does nothing
// with target=_blank, so the page posts "keel:open:<url>" on the same
// message handler as the appearance message (appearance.go), and rawMessage
// hands it here: only from the main frame of the Keel page in Keel's own
// window. The URL is checked again on this side; the page's check is not
// trusted.

// openPrefix starts the page's open message; the URL follows it.
const openPrefix = "keel:open:"

// maxOpenURL bounds a URL the page may ask to open.
const maxOpenURL = 2048

// parseOpenURL reads a page message. ok is false for anything that is not
// an open message with an http, https or mailto URL that parses, has a
// host (http/https) or an address (mailto), no credentials, and no space,
// control or invisible character.
func parseOpenURL(msg string) (target string, ok bool) {
	raw, found := strings.CutPrefix(msg, openPrefix)
	if !found || raw == "" || len(raw) > maxOpenURL {
		return "", false
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
			return "", false
		}
	case "mailto":
		if u.Opaque == "" || strings.HasPrefix(u.Opaque, "/") || u.Host != "" {
			return "", false
		}
	default:
		return "", false
	}
	return u.String(), true
}
