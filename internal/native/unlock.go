package native

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Unlock screen (docs/ui-redesign.md §5.14): the vault exists and keel serve could not open it, or the poll
// found it locked again. It replaces the window while it shows; tabs and
// sessions stay as they are underneath.
//
// The passphrase is typed into a masked field and sent in the one POST /api/vault/unlock. It is cleared from the
// field when the request is sent, and is in no log, error or file.
//
// There is no "Try the keychain again next time" box: it would have no effect (keel serve always asks the keychain
// first at start); its note ("Keel always asks … first") is drawn.

// unlockGate is the Unlock screen's state.
type unlockGate struct {
	vault keelclient.Vault
	pass  string
	err   string
	show  bool // the passphrase is shown, not masked
	busy  bool
	focus bool // give the field the keyboard
	h     ui.Handle
}

// String prints the screen's state without the passphrase.
func (g *unlockGate) String() string { return fmt.Sprintf("unlockGate{err=%q busy=%v}", g.err, g.busy) }

func (g *unlockGate) GoString() string { return g.String() }

const (
	unlockCardWidth = sizeList + space6*2 + space3 // size-list + 2·space-6 + space-3
	markSize        = 48
	sizeList        = 360 // size-list, docs/design/tokens.json
)

// keychainName is what this OS calls the keychain.
func keychainName() string {
	switch platform {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	}
	return "the system keyring"
}

// unlockLead is the line under the title (gate.js showUnlock).
func unlockLead(n int) string {
	switch {
	case n == 1:
		return "Your hosts and keys are encrypted on this machine. 1 session is waiting in keel serve."
	case n > 1:
		return fmt.Sprintf("Your hosts and keys are encrypted on this machine. %d sessions are waiting in keel serve.", n)
	}
	return "Your hosts and keys are encrypted on this machine. keel serve is running."
}

// unlockBanner is the banner text and whether the vault's key is in the keychain (then a passphrase cannot open it).
func unlockBanner(v keelclient.Vault) (msg string, keychainVault bool) {
	kc := keychainName()
	switch {
	case v.Mode == "keychain":
		return kc + " didn't answer, and this vault's key is kept there, so a passphrase can't open it. Allow Keel in " + kc + ", then run keel serve stop and open Keel again.", true
	case v.KeyringOff:
		return "Keel was started with KEEL_KEYRING=off, so it needs your passphrase.", false
	}
	return kc + " doesn't hold this vault's key, so Keel needs your passphrase.", false
}

// gateSessions is the status line's count (gate.js sessionsText).
func gateSessions(n int) string {
	switch {
	case n == 0:
		return "keel serve · no sessions running"
	case n == 1:
		return "keel serve · 1 session waiting"
	}
	return fmt.Sprintf("keel serve · %d sessions waiting", n)
}

// showUnlock puts the Unlock screen over the window.
func (w *workspace) showUnlock(v keelclient.Vault) {
	w.gate = &unlockGate{vault: v, focus: true}
}

// submitUnlock is the Unlock button and ↵ in the field.
func (w *workspace) submitUnlock() {
	g := w.gate
	if g == nil || g.busy {
		return
	}
	if g.pass == "" {
		g.err, g.focus = "Enter the vault passphrase.", true
		return
	}
	pass := g.pass
	g.pass, g.err, g.busy = "", "", true // the field is empty from here on: the passphrase is only in this request
	callErr(w, func(c context.Context) error {
		err := w.be.Unlock(c, pass)
		pass = ""
		return err
	}, func(err error) {
		g.busy = false
		switch {
		case err == nil:
			w.unlocked(g)
		case errors.Is(err, keelclient.ErrPassphrase):
			g.err, g.focus = "That passphrase doesn't open the vault.", true
		default:
			g.err, g.focus = err.Error(), true
		}
	})
}

// unlocked leaves the Unlock screen: a window that never started starts now; a window that has its tabs
// reads the hosts again.
func (w *workspace) unlocked(g *unlockGate) {
	if w.gate != g {
		return
	}
	w.gate = nil
	if w.live {
		w.reloadHosts()
		w.refreshSuggestSettings()
		return
	}
	w.start()
}

// unlockView is the screen: the title bar's drag strip, the card and the status line.
func (w *workspace) unlockView(c *ui.Context) {
	g := w.gate
	hairline(ui.Row(c).FillWidth().Height(sizeTitlebar).Shrink(0).Background(w.ground(chrome)), 0, 0, 1, 0).DragWindow()
	ui.Column(c).FillWidth().Grow(1).Shrink(1).Center().Padding(space6, space4).Background(w.ground(stage)).Label("unlock").Children(func() {
		ui.Column(c).Width(unlockCardWidth).Gap(space4 + space0_5).Children(func() { w.unlockCard(c, g) })
	})
	hairline(ui.Row(c).FillWidth().Height(statusbarSize).Shrink(0).Padding(0, space3).Gap(space4).Background(w.ground(chrome)), 1, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(space1_5).Children(func() {
			dot(c, tide)
			caption(c, gateSessions(g.vault.Sessions), inkMuted)
		})
		ui.Spacer(c)
		mono(c, "keel.vault · age encrypted", inkMuted)
	})
	if _, keychainVault := unlockBanner(g.vault); g.focus && !keychainVault {
		g.h.Focus(c)
		g.focus = false
	}
}

func (w *workspace) unlockCard(c *ui.Context, g *unlockGate) {
	msg, keychainVault := unlockBanner(g.vault)
	mark(c)
	ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
		ui.Text(c, "Unlock your vault").TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold)
		ui.Text(c, unlockLead(g.vault.Sessions)).TextColor(inkMuted).FontSize(uiFontSize)
	})
	soft, fg := hazeSoft, haze
	if keychainVault {
		soft, fg = signalSoft, signal
	}
	ui.Row(c).FillWidth().Padding(space1_5, space3).Gap(space2).Radius(radiusSm).Background(soft).Label("unlock banner").Children(func() {
		icon(c, "alert", fg)
		ui.Text(c, msg).TextColor(fg).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
	})
	if keychainVault {
		return // the key is in the keychain: there is nothing to type
	}
	ui.Column(c).FillWidth().Gap(space1).Children(func() {
		label(c, "Vault passphrase", inkMuted)
		border := lineStrong
		if g.err != "" {
			border = flare
		}
		ui.Row(c).FillWidth().Height(sizeControlLg+space1).Padding(0, space1, 0, space2).Gap(space2).Radius(radiusSm).Background(stage).Border(1, border).Children(func() {
			icon(c, "lock", inkFaint)
			in := ui.TextInputBase(c, &g.pass).Label("Vault passphrase").TextColor(ink).FontSize(titleSize).Grow(1).MinWidth(0).Bind(&g.h)
			if !g.show {
				in.Password()
			}
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					g.pass = k.Text()
					w.act(w.submitUnlock)
					return true
				}
				return false
			})
			tip := "Show passphrase"
			if g.show {
				tip = "Hide passphrase"
			}
			if iconBtn(c, "eye", tip, sizeControlSm, g.show) {
				w.act(func() { g.show, g.focus = !g.show, true })
			}
		})
		if g.err != "" {
			ui.Row(c).Gap(space1).Label("unlock error").Children(func() { caption(c, g.err, flare) })
		}
	})
	if !g.vault.KeyringOff {
		caption(c, "Keel always asks "+keychainName()+" first when keel serve starts.", inkMuted)
	}
	if capButton(c, "Unlock", "↵", false).FillWidth().Clicked() {
		w.act(w.submitUnlock)
	}
	ui.Text(c, "Forgot it? Keel can't recover the vault without the passphrase. Sessions already running in keel serve keep working while the vault is locked.").
		TextColor(inkMuted).FontSize(captionSize)
}

// markPath is the Keel mark: the keel on a tile. The tile is ink and the keel is stage, so no colour literal is added.
const markPath = `m90.3 25.8-37.6-21.8c-1.5-0.9-3.3-1-4.9-0.2l-33.2 18.9c-1.5 0.8-1.5 3 0 3.8l36.6 21.4c1.7 0.9 1.7 3.3 0 4.2-0.7 0.5-1.6 0.4-2.4 0l-36.8-21.7c-1.5-0.9-3.2 0.2-3.2 1.8v40.1c0 1 0.6 2.1 1.5 2.7l38.1 21.4c1 0.6 2.2 0.7 3.2 0.1l38.6-21.4c0.9-0.6 1.3-1.6 1.3-2.7v-44.5c-0.1-0.8-0.4-1.5-1.2-2.1z`

var markSVG *ui.SVG

func mark(c *ui.Context) {
	if markSVG == nil {
		markSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32"><path fill="currentColor" transform="translate(4,5.5) scale(0.24)" d="` + markPath + `"/></svg>`))
	}
	ui.Box(c).Size(markSize, markSize).Radius(radiusLg).Background(ink).Shrink(0).Label("Keel").Children(func() {
		ui.Icon(c, markSVG).FontSize(markSize).TextColor(stage)
	})
}
