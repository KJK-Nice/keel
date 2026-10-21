package desktop

import "testing"

func TestParseOpenURLTakesOnlyWebAndMailLinks(t *testing.T) {
	for msg, want := range map[string]string{
		"keel:open:https://example.com/a?b=1#c": "https://example.com/a?b=1#c",
		"keel:open:http://example.com":          "http://example.com",
		"keel:open:HTTPS://example.com/":        "https://example.com/",
		"keel:open:mailto:ops@example.com":      "mailto:ops@example.com",
	} {
		got, ok := parseOpenURL(msg)
		if !ok || got != want {
			t.Errorf("parseOpenURL(%q) = %q, %v; want %q", msg, got, ok, want)
		}
	}
	for _, msg := range []string{
		"",
		"keel:open:",
		"https://example.com",
		"keel:appearance:dark",
		"keel:open:javascript:alert(1)",
		"keel:open:JavaScript:alert(1)",
		"keel:open:data:text/html,<script>alert(1)</script>",
		"keel:open:file:///etc/passwd",
		"keel:open:vbscript:x",
		"keel:open:ssh://host",
		"keel:open:x-apple.systempreferences:com.apple.preference",
		"keel:open:/relative",
		"keel:open://example.com",
		"keel:open:https://",
		"keel:open:https://user:pass@example.com/",
		"keel:open:https://example.com/a b",
		"keel:open:https://example.com/\n",
		"keel:open:https://exa\u200bmple.com/",
		"keel:open:https://example.com/\u202e",
		"keel:open:mailto:",
		"keel:open:mailto://x",
		"keel:open:https://example.com/" + string(make([]byte, 2100)),
	} {
		if got, ok := parseOpenURL(msg); ok {
			t.Errorf("parseOpenURL(%q) accepted %q", msg, got)
		}
	}
}
