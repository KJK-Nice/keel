package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"keel/internal/store"
)

func renderString(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestIdentityRowMenu(t *testing.T) {
	key := store.Identity{ID: "k1", Name: "deploy", Kind: "key", Username: "ops", PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----", Passphrase: "pp-secret"}
	pw := store.Identity{ID: "p1", Name: "root pw", Kind: "password", Password: "pw-secret"}
	hosts := []store.Host{{ID: "h1", AuthType: "identity", IdentityID: "k1"}}
	if !identityInUse(hosts, key) || identityInUse(hosts, pw) {
		t.Fatal("identityInUse: a host uses k1, none uses p1")
	}
	for _, id := range []store.Identity{key, pw} {
		out := renderString(t, identityRowMenu(hosts, id))
		for _, want := range []string{`data-edit-identity="` + id.ID + `"`, `data-delete-identity="` + id.ID + `"`, "k-menu-item", "k-menu-item--danger", `class="k-i`} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: menu lacks %q", id.ID, want)
			}
		}
		for _, secret := range []string{"BEGIN", "pp-secret", "pw-secret"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s: menu holds a vault value", id.ID)
			}
		}
		// The row is one button already: no visible "…" trigger.
		if !strings.Contains(out, `hidden tabindex="-1" aria-label="Identity actions"`) {
			t.Errorf("%s: trigger is not hidden", id.ID)
		}
	}
	if !strings.Contains(renderString(t, identityRowMenu(hosts, key)), " disabled") {
		t.Error("Delete must be disabled for an identity a host uses")
	}
	if strings.Contains(renderString(t, identityRowMenu(hosts, pw)), " disabled") {
		t.Error("Delete must be enabled for an identity no host uses")
	}
}

func TestIdentityAndSnippetRowsHaveMenus(t *testing.T) {
	p := PageProps{
		Identities: []store.Identity{{ID: "k1", Name: "deploy", Kind: "key"}},
		Snippets:   []store.Snippet{{ID: "s1", Name: "disk", Command: "df -h"}},
	}
	ids := renderString(t, IdentityList(p))
	if !strings.Contains(ids, `class="k-item-row" data-row-menu`) || !strings.Contains(ids, `data-delete-identity="k1"`) {
		t.Error("Keychain rows need data-row-menu and their menu")
	}
	sn := renderString(t, SnippetList(p))
	for _, want := range []string{`class="k-item-row" data-row-menu`, `data-snippet-run="s1"`, `data-edit-snippet="s1"`, `data-delete-snippet="s1"`} {
		if !strings.Contains(sn, want) {
			t.Errorf("Snippets page row lacks %q", want)
		}
	}
}

func TestTermMenu(t *testing.T) {
	out := renderString(t, TermMenu())
	last := -1
	for _, a := range []string{"copy", "paste", "select-all", "clear", "find", "split-h", "split-v", "close"} {
		i := strings.Index(out, `data-term-act="`+a+`"`)
		if i < 0 {
			t.Fatalf("terminal menu lacks %s", a)
		}
		if i < last {
			t.Errorf("terminal menu: %s out of order", a)
		}
		last = i
	}
	// Real shortcuts, localised by lib/rowmenu.js through data-keys.
	for _, want := range []string{`data-keys="copy"`, `data-keys="paste"`, `data-keys="select-all"`, `data-keys="find-term"`, `data-keys="split-right"`, `data-keys="split-down"`, `data-keys="close-tab"`, `id="term-menu-trigger"`} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal menu lacks %q", want)
		}
	}
}

func TestFenceAndCardMenus(t *testing.T) {
	fence := renderString(t, FenceMenu())
	for _, want := range []string{`id="fence-menu-trigger"`, `data-fence-act="type"`, `data-fence-act="copy"`, "data-fence-type-label"} {
		if !strings.Contains(fence, want) {
			t.Errorf("fence menu lacks %q", want)
		}
	}
	card := renderString(t, CardMenu())
	for _, want := range []string{`id="card-menu-trigger"`, `data-card-menu-items="true"`, `role="menu"`} {
		if !strings.Contains(card, want) {
			t.Errorf("card menu lacks %q", want)
		}
	}
}
