package native

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"keel/internal/native/keelclient"
)

// The pure helpers of Settings (tested in settingshelpers_test.go). No UI here.

// uptimeText formats a runtime's uptime: "45 s", "12 min", "3 h 05 min", "2 d 4 h".
func uptimeText(sec int64) string {
	s := max(sec, 0)
	if s < 60 {
		return fmt.Sprintf("%d s", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	h := m / 60
	if h < 24 {
		return fmt.Sprintf("%d h %02d min", h, m%60)
	}
	return fmt.Sprintf("%d d %d h", h/24, h%24)
}

// versionOf is the revision part of a build id "<revision>-<web hash>", shortened to 7
// characters ("dev" when the binary has no VCS info).
func versionOf(build string) string {
	rev, _, _ := strings.Cut(build, "-")
	if rev == "" {
		return ""
	}
	base, dirty := strings.CutSuffix(rev, "+dirty")
	if utf8.RuneCountInString(base) > 7 {
		base = string([]rune(base)[:7])
	}
	if dirty {
		return base + "+dirty"
	}
	return base
}

// displayName is the custom name when set, else the default, else "guest".
func displayName(custom, fallback string) string {
	if s := strings.TrimSpace(custom); s != "" {
		return s
	}
	if s := strings.TrimSpace(fallback); s != "" {
		return s
	}
	return "guest"
}

// nameMax is the display name's length.
const nameMax = 40

// vaultMode is how the vault is unlocked, from GET /api/vault.
func vaultMode(v keelclient.Vault) (label, help string) {
	switch v.Mode {
	case "keychain":
		return "OS keychain", "The key that opens the vault is kept in your operating system's keychain. No passphrase to type."
	case "passphrase":
		if v.KeyringOff {
			return "Passphrase", "The OS keychain is off for this run (KEEL_KEYRING=off), so Keel asks for the passphrase."
		}
		return "Passphrase", "Keel asks for the passphrase when it starts."
	}
	return "Not set up", "The vault is created on first run."
}

// signalFields is the Signal server field from /api/runtime or PUT /api/share/signal: the
// input's value (the saved server), its placeholder and the help line.
func signalFields(info keelclient.Runtime) (value, placeholder, help string) {
	url, saved := strings.TrimSpace(info.SignalURL), strings.TrimSpace(info.SignalSaved)
	placeholder = "https://signal.example.com"
	relay := "Join links go through this server. It never sees the terminal or the key in the link's #fragment."
	switch info.SignalSource {
	case "env":
		return saved, placeholder, "KEEL_SIGNAL is set for this run, so Keel uses " + url + ". The server saved here is used when it is not set."
	case "settings":
		return saved, placeholder, relay
	case "build":
		return saved, url, "This copy of Keel was built with " + url + ". Enter another server to use it instead. " + relay
	}
	return saved, placeholder, "Share is off until you enter a keel-signal server: one you run yourself, or one someone runs for you."
}

// stopMessage is the alertdialog text for Stop runtime.
func stopMessage(sessions int) string {
	what := "every running session"
	switch {
	case sessions == 1:
		what = "the 1 running session"
	case sessions > 1:
		what = fmt.Sprintf("all %d running sessions", sessions)
	}
	return "This ends " + what + " and every port forward, then closes keel serve. Reopen Keel to start it again."
}

// shortcut is a row of the Keyboard page: made from the command table, with the key cap text of this platform.
type shortcut struct{ group, title, keys string }

var shortcutGroupNames = []string{"General", "Tabs", "Vault", "Panes", "Terminal", "Agent"}

// shortcutGroup is a group of the Keyboard page.
type shortcutGroup struct {
	name  string
	items []shortcut
}

// shortcutGroups are the commands that have a row on the Keyboard page, in group order and the table's order inside a
// group; a group with none is left out.
func shortcutGroups() (out []shortcutGroup) {
	for _, g := range shortcutGroupNames {
		var items []shortcut
		for i := range commands {
			if h := commands[i].help; h != nil && h.group == g {
				items = append(items, shortcut{g, h.title, commands[i].keyLabel()})
			}
		}
		if len(items) > 0 {
			out = append(out, shortcutGroup{g, items})
		}
	}
	return out
}

// splitKeyCaps splits a label split into the caps to draw: "Ctrl+Shift+I" is Ctrl, Shift, I; "⌘⇧T" is
// ⌘, ⇧, T; "Ctrl++" is Ctrl, +.
func splitKeyCaps(label string) (caps []string) {
	rest := label
	for _, mod := range []string{"Ctrl+", "Shift+"} {
		if strings.HasPrefix(rest, mod) && len(rest) > len(mod) {
			caps = append(caps, strings.TrimSuffix(mod, "+"))
			rest = rest[len(mod):]
		}
	}
	chars := []rune(rest)
	isMod := func(r rune) bool { return strings.ContainsRune("⌘⇧⌥⌃", r) }
	switch {
	case len(chars) > 1 && isMod(chars[0]):
		i := 0
		for i < len(chars)-1 && isMod(chars[i]) {
			caps = append(caps, string(chars[i]))
			i++
		}
		caps = append(caps, string(chars[i:]))
	case rest != "":
		caps = append(caps, rest)
	}
	return caps
}
