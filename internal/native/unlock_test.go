package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// A made-up passphrase for the fake backend: it opens nothing.
const testPass = "fake-pass-for-tests"

func lockedBackend() *fakeBackend {
	return &fakeBackend{vault: keelclient.Vault{Locked: true, Path: "~/keel/keel.vault", Sessions: 2}, hosts: testHosts, unlockWant: testPass}
}

func TestUnlockScreenIsShownForALockedVault(t *testing.T) {
	fb := lockedBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	for _, s := range []string{
		"Unlock your vault",
		"Your hosts and keys are encrypted on this machine. 2 sessions are waiting in keel serve.",
		"Vault passphrase", "Unlock",
		"Forgot it? Keel can't recover the vault without the passphrase. Sessions already running in keel serve keep working while the vault is locked.",
		"keel serve · 2 sessions waiting", "keel.vault · age encrypted",
	} {
		if !r.tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	if !r.tt.Focused("Vault passphrase") {
		t.Error("the passphrase field does not have the keyboard")
	}
	if r.tt.HasText("Filter hosts") || r.tt.HasText("Welcome to Keel") {
		t.Error("the workspace or First run shows under the Unlock screen")
	}
	if !slices.Equal(fb.calls, []string{"vault"}) {
		t.Errorf("calls %v", fb.calls)
	}
}

func TestUnlockBannerAndStatesFollowTheVaultMode(t *testing.T) {
	kc := keychainName()
	for _, c := range []struct {
		v          keelclient.Vault
		banner     string
		field      bool
		keychainFn bool
	}{
		{keelclient.Vault{Locked: true, Mode: "keychain"}, kc + " didn't answer, and this vault's key is kept there, so a passphrase can't open it. Allow Keel in " + kc + ", then run keel serve stop and open Keel again.", false, false},
		{keelclient.Vault{Locked: true, KeyringOff: true}, "Keel was started with KEEL_KEYRING=off, so it needs your passphrase.", true, false},
		{keelclient.Vault{Locked: true}, kc + " doesn't hold this vault's key, so Keel needs your passphrase.", true, true},
	} {
		fb := &fakeBackend{vault: c.v}
		r := newRig(t, fb)
		r.tt.Frame()
		if !r.tt.HasText(c.banner) {
			t.Errorf("%+v: no banner %q", c.v, c.banner)
		}
		if r.has("Vault passphrase") != c.field || r.has("Unlock") != c.field {
			t.Errorf("%+v: field shown %v, want %v", c.v, r.has("Vault passphrase"), c.field)
		}
		if got := r.tt.HasText("Keel always asks " + kc + " first when keel serve starts."); got != c.keychainFn {
			t.Errorf("%+v: keychain note shown %v, want %v", c.v, got, c.keychainFn)
		}
	}
}

func (r *rig) typeIn(s string) {
	r.tt.Type(s)
	r.tt.Frame()
}

func TestWrongPassphraseShowsTheErrorAndKeepsFocus(t *testing.T) {
	fb := lockedBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.typeIn("not-" + testPass)
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if !r.tt.HasText("That passphrase doesn't open the vault.") {
		t.Fatal("no error text")
	}
	if !r.tt.Focused("Vault passphrase") {
		t.Error("focus left the field")
	}
	if r.w.gate == nil || r.w.gate.pass != "" {
		t.Error("the wrong passphrase is still in the field's state")
	}
	if fb.unlockCalls != 1 || r.w.tabCount() != 0 || fb.hostsCalls != 0 {
		t.Errorf("unlock calls %d, tabs %d, hosts calls %d", fb.unlockCalls, r.w.tabCount(), fb.hostsCalls)
	}
	// Typing again after the error works, and the right one opens the window.
	r.typeIn(testPass)
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.gate != nil || !r.tt.HasText("Filter hosts") || !r.tt.HasText("Local Shell") {
		t.Fatalf("gate %v, workspace shown %v", r.w.gate != nil, r.tt.HasText("Filter hosts"))
	}
}

func TestEmptyPassphraseIsRefusedWithoutARequest(t *testing.T) {
	fb := lockedBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "Unlock")
	if !r.tt.HasText("Enter the vault passphrase.") || fb.unlockCalls != 0 {
		t.Fatalf("calls %d", fb.unlockCalls)
	}
}

func TestRightPassphraseLoadsTheWorkspaceAndKeepsNoPassphrase(t *testing.T) {
	fb := lockedBackend()
	fb.sessions = []keelclient.Session{{ID: "s1", HostID: "h-api", Status: "ready"}}
	r := newRig(t, fb)
	r.tt.Frame()
	r.typeIn(testPass)
	r.click(t, "Unlock") // the button submits too
	r.tt.Frame()
	if r.w.gate != nil {
		t.Fatal("the Unlock screen is still up")
	}
	if !slices.Equal(fb.attached, []string{"s1"}) || !r.tt.HasText("api-1") {
		t.Fatalf("attached %v: hosts and sessions were not loaded as start does", fb.attached)
	}
	if got := fmt.Sprintf("%+v", r.w) + fmt.Sprintf("%+v", r.w.gate); strings.Contains(got, testPass) {
		t.Error("the passphrase is on the workspace after the request")
	}
	// The screen's state held it only until the request went out.
	if fb.unlockCalls != 1 {
		t.Errorf("%d unlock calls", fb.unlockCalls)
	}
}

func TestShowPassphraseTogglesTheMask(t *testing.T) {
	r := newRig(t, lockedBackend())
	r.tt.Frame()
	if r.w.gate.show {
		t.Fatal("shown from the start")
	}
	r.click(t, "Show passphrase")
	if !r.w.gate.show || !r.has("Hide passphrase") {
		t.Error("the passphrase was not shown")
	}
	if !r.tt.Focused("Vault passphrase") {
		t.Error("the field lost the keyboard")
	}
}

func TestSetupAndFirstRunShowFirstRunNotUnlock(t *testing.T) {
	for _, v := range []keelclient.Vault{{Locked: true, NeedsSetup: true}, {FirstRun: true}} {
		r := newRig(t, &fakeBackend{vault: v})
		r.tt.Frame()
		if !r.tt.HasText("Welcome to Keel") || r.has("Vault passphrase") {
			t.Errorf("%+v", v)
		}
	}
}

func TestNoShortcutActsOnTheUnlockScreen(t *testing.T) {
	fb := lockedBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.press(ui.Cmd, ui.KeyT)
	r.press(ui.Cmd, ui.KeyK)
	if r.w.tabCount() != 0 || r.w.pal != nil {
		t.Error("a window shortcut acted over the Unlock screen")
	}
}

func TestLockedFirstRunShowsUnlockThenFirstRun(t *testing.T) {
	fb := &fakeBackend{vault: keelclient.Vault{Locked: true, FirstRun: true}, hosts: testHosts, unlockWant: testPass}
	fb.unlockWant = testPass
	r := newRig(t, fb)
	r.tt.Frame()
	if !r.tt.HasText("Unlock your vault") {
		t.Fatal("a locked vault with an open first run does not show Unlock")
	}
	fb.vault.FirstRun = true // Unlock opens it; the first run stays open
	r.typeIn(testPass)
	r.press(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.gate != nil || !r.tt.HasText("Welcome to Keel") {
		t.Error("after unlocking, an open first run shows its step 1")
	}
}
