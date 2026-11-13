package cmdrule

import "testing"

// §6 rule 13: only a command that stays one line on a prompt, as typed,
// may go without the approve dialog.
func TestDirectLine(t *testing.T) {
	for _, ok := range []string{"ls -la", "echo 'h\xc3\xa9llo' | wc -c", "kubectl get pods -n kube-system", "df -h \\"} {
		if !DirectLine(ok) {
			t.Errorf("DirectLine(%q) = false", ok)
		}
	}
	for _, bad := range []string{
		"", "ls\n", "ls\npwd", "ls\r", "ls\ttab", "ls\x1b[A", "ls\x00", "ls\x7f", "\x15ls",
		"ls\xc2\x85",        // NEL, a C1 control
		"ls\xc2\x9b",        // CSI
		"ls\xe2\x80\xa8pwd", // line separator
		"ls\xe2\x80\xa9",    // paragraph separator
		"ls\xe2\x80\x8b",    // zero-width space
		"ls\xe2\x80\xae",    // right-to-left override
		"ls\xef\xbb\xbf",    // byte order mark
		"ls\xff",            // not UTF-8
	} {
		if DirectLine(bad) {
			t.Errorf("DirectLine(%q) = true", bad)
		}
	}
}

func TestPlaceholder(t *testing.T) {
	for _, s := range []string{Placeholder(PlaceholderSecret, 1), Placeholder(PlaceholderIP, 12), Placeholder(PlaceholderHost, 3), "curl [secret:2]/x", "export T=[secret:1]", "ping [ip:12]", "ssh [host:3]"} {
		if !HasPlaceholder(s) {
			t.Errorf("HasPlaceholder(%q) = false", s)
		}
	}
	for _, s := range []string{"", "[secret]", "[secret:]", "[other:1]", "{{name}}", "ls [a-z]", "ls [abc]", "echo ok", "ls [abc] [secret:]"} {
		if HasPlaceholder(s) {
			t.Errorf("HasPlaceholder(%q) = true", s)
		}
	}
	if Placeholder(PlaceholderSecret, 1) != "[secret:1]" {
		t.Fatal("placeholder text changed")
	}
}
