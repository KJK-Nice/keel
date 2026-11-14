package native

import (
	"slices"
	"testing"

	"keel/internal/native/keelclient"
)

func TestSettingsFormatting(t *testing.T) {
	for sec, want := range map[int64]string{5: "5 s", 125: "2 min", 3*3600 + 300: "3 h 05 min", 2*86400 + 4*3600: "2 d 4 h", -3: "0 s", 59: "59 s", 60: "1 min"} {
		if got := uptimeText(sec); got != want {
			t.Errorf("uptime %d = %q, want %q", sec, got, want)
		}
	}
	for build, want := range map[string]string{"9d62912abcde-0123456789ab": "9d62912", "9d62912abcde+dirty-0123": "9d62912+dirty", "dev-abc": "dev", "": "", "abc": "abc"} {
		if got := versionOf(build); got != want {
			t.Errorf("version %q = %q, want %q", build, got, want)
		}
	}
	if displayName(" Ann ", "bob") != "Ann" || displayName("", "bob") != "bob" || displayName("", "") != "guest" {
		t.Error("display name")
	}
	if stopMessage(3) != "This ends all 3 running sessions and every port forward, then closes keel serve. Reopen Keel to start it again." ||
		stopMessage(1) != "This ends the 1 running session and every port forward, then closes keel serve. Reopen Keel to start it again." ||
		stopMessage(0) != "This ends every running session and every port forward, then closes keel serve. Reopen Keel to start it again." {
		t.Errorf("stop messages %q", stopMessage(3))
	}
}

func TestVaultModeText(t *testing.T) {
	if l, _ := vaultMode(keelclient.Vault{Mode: "keychain"}); l != "OS keychain" {
		t.Error(l)
	}
	if _, h := vaultMode(keelclient.Vault{Mode: "passphrase", KeyringOff: true}); h != "The OS keychain is off for this run (KEEL_KEYRING=off), so Keel asks for the passphrase." {
		t.Error(h)
	}
	if l, h := vaultMode(keelclient.Vault{Mode: "passphrase"}); l != "Passphrase" || h != "Keel asks for the passphrase when it starts." {
		t.Error(l, h)
	}
	if l, h := vaultMode(keelclient.Vault{}); l != "Not set up" || h != "The vault is created on first run." {
		t.Error(l, h)
	}
}

func TestSignalFields(t *testing.T) {
	v, ph, help := signalFields(keelclient.Runtime{})
	if v != "" || ph != "https://signal.example.com" || help != "Share is off until you enter a keel-signal server: one you run yourself, or one someone runs for you." {
		t.Errorf("no server: %q %q %q", v, ph, help)
	}
	v, _, help = signalFields(keelclient.Runtime{SignalURL: "https://x.example", SignalSource: "settings", SignalSaved: "https://x.example"})
	if v != "https://x.example" || help != "Join links go through this server. It never sees the terminal or the key in the link's #fragment." {
		t.Errorf("saved: %q %q", v, help)
	}
	v, _, help = signalFields(keelclient.Runtime{SignalURL: "https://env.example", SignalSource: "env", SignalSaved: "https://x.example"})
	if v != "https://x.example" || help != "KEEL_SIGNAL is set for this run, so Keel uses https://env.example. The server saved here is used when it is not set." {
		t.Errorf("env: %q %q", v, help)
	}
	v, ph, help = signalFields(keelclient.Runtime{SignalURL: "https://baked.example", SignalSource: "build"})
	if v != "" || ph != "https://baked.example" || help[:len("This copy of Keel was built with https://baked.example.")] != "This copy of Keel was built with https://baked.example." {
		t.Errorf("build: %q %q %q", v, ph, help)
	}
}

func TestKeyCapsSplitAndShortcutTitlesAreUnique(t *testing.T) {
	for label, want := range map[string][]string{"Ctrl+Shift+I": {"Ctrl", "Shift", "I"}, "⌘⇧T": {"⌘", "⇧", "T"}, "Ctrl++": {"Ctrl", "+"}, "⌘,": {"⌘", ","}, "Ctrl+⇧↵": {"Ctrl", "⇧", "↵"}} {
		if got := splitKeyCaps(label); !slices.Equal(got, want) {
			t.Errorf("caps %q = %v, want %v", label, got, want)
		}
	}
	titles := map[string]bool{}
	for _, g := range shortcutGroups() {
		for _, s := range g.items {
			if titles[s.title] {
				t.Errorf("%q twice", s.title)
			}
			titles[s.title] = true
		}
	}
}
