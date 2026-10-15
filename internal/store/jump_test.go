package store

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func mustHost(t *testing.T, st *Store, h Host) Host {
	t.Helper()
	if h.AuthType == "" {
		h.AuthType = "password"
	}
	if h.Username == "" {
		h.Username = "u"
	}
	saved, err := st.UpsertHost(h)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestJumpHostRules(t *testing.T) {
	st := testStore(t)
	bastion := mustHost(t, st, Host{Name: "bastion", Address: "10.0.0.1", Password: "jump-secret-pw"})
	db := mustHost(t, st, Host{Name: "db", Address: "10.0.4.21", JumpHost: bastion.ID, Password: "db-pw"})
	if db.JumpHost != bastion.ID {
		t.Fatalf("jumpHost = %q", db.JumpHost)
	}

	cases := []struct {
		name string
		h    Host
		want string
	}{
		{"self", Host{ID: db.ID, Name: "db", Address: "10.0.4.21", JumpHost: db.ID}, "its own jump host"},
		{"cycle", Host{ID: bastion.ID, Name: "bastion", Address: "10.0.0.1", JumpHost: db.ID}, "one hop"},
		{"second hop", Host{Name: "deep", Address: "10.0.9.9", JumpHost: db.ID}, "only one hop"},
		{"missing", Host{Name: "x", Address: "10.0.9.9", JumpHost: "nope"}, "not found"},
	}
	locals, _ := st.ListHosts()
	cases = append(cases, struct {
		name string
		h    Host
		want string
	}{"local jump", Host{Name: "y", Address: "10.0.9.9", JumpHost: locals[0].ID}, "local shell"})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.h.AuthType == "" {
				c.h.AuthType = "password"
				c.h.Username = "u"
			}
			_, err := st.UpsertHost(c.h)
			if err == nil || !errors.Is(err, ErrHostField) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// A rejected save changed nothing.
	got, err := st.GetHost(bastion.ID)
	if err != nil || got.JumpHost != "" {
		t.Fatalf("bastion = %+v, %v", got, err)
	}
}

func TestHostForConnectResolvesJump(t *testing.T) {
	st := testStore(t)
	ident, err := st.UpsertIdentity(Identity{Name: "deploy", Kind: "password", Username: "jumper", Password: "ident-pw"})
	if err != nil {
		t.Fatal(err)
	}
	bastion := mustHost(t, st, Host{Name: "bastion", Address: "10.0.0.1", AuthType: "identity", IdentityID: ident.ID, Username: " "})
	db := mustHost(t, st, Host{Name: "db", Address: "10.0.4.21", JumpHost: bastion.ID, Password: "db-pw"})
	h, err := st.HostForConnect(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Jump == nil || h.Jump.ID != bastion.ID || h.Jump.Password != "ident-pw" || h.Jump.AuthType != "password" {
		t.Fatalf("jump = %+v", h.Jump)
	}
	if h.Password != "db-pw" {
		t.Fatal("target secret not loaded")
	}

	// keel.json holds the reference only, never a secret.
	b, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"jumpHost": "`+bastion.ID+`"`) {
		t.Fatalf("jumpHost not saved: %s", b)
	}
	for _, secret := range []string{"ident-pw", "db-pw"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("keel.json holds %q", secret)
		}
	}

	// Deleting the jump host leaves a clear error, not a direct connect.
	if err := st.DeleteHost(bastion.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.HostForConnect(db.ID); err == nil || !strings.Contains(err.Error(), "jump host not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveDraftUsesSavedSecretsOnlyForTheSavedTarget(t *testing.T) {
	st := testStore(t)
	bastion := mustHost(t, st, Host{Name: "bastion", Address: "10.0.0.1", Password: "jump-pw"})
	other := mustHost(t, st, Host{Name: "other", Address: "10.0.0.2", Password: "other-pw"})
	db := mustHost(t, st, Host{Name: "db", Address: "10.0.4.21", Port: 22, Username: "deploy", Password: "saved-pw", JumpHost: bastion.ID})
	base := Host{ID: db.ID, Name: "db", Address: "10.0.4.21", Port: 22, Username: "deploy", AuthType: "password", Password: "__saved__", JumpHost: bastion.ID}

	// Same target: the blank password comes from the vault.
	h, err := st.ResolveDraft(base)
	if err != nil {
		t.Fatal(err)
	}
	if h.Password != "saved-pw" || h.Jump == nil || h.Jump.Password != "jump-pw" {
		t.Fatalf("draft = %+v jump=%+v", h, h.Jump)
	}
	// Case and an implied port 22 are the same target.
	same := base
	same.Address, same.Port = "10.0.4.21", 0
	if h, err := st.ResolveDraft(same); err != nil || h.Password != "saved-pw" {
		t.Fatalf("same target: %+v, %v", h, err)
	}

	// Any changed connection field: the saved password is not used.
	changes := map[string]func(*Host){
		"Address changed":        func(d *Host) { d.Address = "203.0.113.9" },
		"Port changed":           func(d *Host) { d.Port = 2222 },
		"Username changed":       func(d *Host) { d.Username = "root" },
		"Authentication changed": func(d *Host) { d.AuthType = "key" },
		"Jump host changed":      func(d *Host) { d.JumpHost = other.ID },
	}
	for want, change := range changes {
		d := base
		change(&d)
		_, err := st.ResolveDraft(d)
		if err == nil || !errors.Is(err, ErrHostField) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v", want, err)
		}
		// Typing the secret makes it testable, with the typed one only.
		d.Password, d.PrivateKey = "typed", "typed-key"
		h, err := st.ResolveDraft(d)
		if err != nil {
			t.Fatalf("%s typed: %v", want, err)
		}
		if h.Password == "saved-pw" || h.Passphrase != "" {
			t.Fatalf("%s: saved secret used: %+v", want, h)
		}
	}
	// No jump host at all is a change too.
	d := base
	d.JumpHost = ""
	if _, err := st.ResolveDraft(d); err == nil || !strings.Contains(err.Error(), "Jump host changed") {
		t.Fatalf("jump removed: %v", err)
	}

	// An id that is not saved gets no vault secret.
	d = base
	d.ID = "nope"
	if _, err := st.ResolveDraft(d); err == nil || !strings.Contains(err.Error(), "not saved") {
		t.Fatalf("unknown id: %v", err)
	}

	// Nothing is saved by a draft.
	saved, _ := st.GetHost(db.ID)
	if saved.Password != "saved-pw" || saved.Address != "10.0.4.21" || saved.JumpHost != bastion.ID {
		t.Fatalf("ResolveDraft saved something: %+v", saved)
	}
	// Self reference is rejected for drafts too.
	d = base
	d.Password, d.JumpHost = "typed", db.ID
	if _, err := st.ResolveDraft(d); err == nil {
		t.Fatal("self jump accepted")
	}
}

func TestResolveDraftIdentityOnlyForTheSavedTarget(t *testing.T) {
	st := testStore(t)
	ident, err := st.UpsertIdentity(Identity{Name: "deploy", Kind: "password", Username: "deploy", Password: "ident-pw"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.UpsertIdentity(Identity{Name: "root", Kind: "password", Username: "root", Password: "root-pw"})
	if err != nil {
		t.Fatal(err)
	}
	db := mustHost(t, st, Host{Name: "db", Address: "10.0.4.21", AuthType: "identity", IdentityID: ident.ID, Username: "deploy"})
	base := Host{ID: db.ID, Address: "10.0.4.21", Port: 22, Username: "deploy", AuthType: "identity", IdentityID: ident.ID}
	h, err := st.ResolveDraft(base)
	if err != nil || h.Password != "ident-pw" {
		t.Fatalf("same target: %+v, %v", h, err)
	}
	for name, d := range map[string]Host{
		"new address":    {ID: db.ID, Address: "203.0.113.9", Port: 22, Username: "deploy", AuthType: "identity", IdentityID: ident.ID},
		"other identity": {ID: db.ID, Address: "10.0.4.21", Port: 22, Username: "deploy", AuthType: "identity", IdentityID: other.ID},
		"unsaved host":   {Address: "203.0.113.9", Port: 22, Username: "deploy", AuthType: "identity", IdentityID: ident.ID},
	} {
		if _, err := st.ResolveDraft(d); err == nil || !strings.Contains(err.Error(), "Keychain identity") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestTrustOfferedOnlyForTheRefusedKey(t *testing.T) {
	st := testStore(t)
	const fp = "SHA256:q3VtXz0b6kP1d8yRk2mJw4sTn7aEc9hLf5uGi3o9fKe"
	const fp2 = "SHA256:AAAAXz0b6kP1d8yRk2mJw4sTn7aEc9hLf5uGi3o9fKe"
	var unk *UnknownHostError
	if err := st.CheckHostKeyType("10.0.0.5:22", "ssh-ed25519", fp); !errors.As(err, &unk) || unk.Token == "" || unk.KeyType != "ssh-ed25519" {
		t.Fatalf("err = %v", err)
	}
	// Wrong address, wrong key, unknown token, replace on an unknown key.
	for _, c := range []struct {
		tok, host, fp string
		replace       bool
	}{
		{unk.Token, "10.0.0.6:22", fp, false},
		{unk.Token, "10.0.0.5:22", fp2, false},
		{"nope", "10.0.0.5:22", fp, false},
		{unk.Token, "10.0.0.5:22", fp, true},
	} {
		if _, err := st.TrustOffered(c.tok, c.host, c.fp, c.replace); err == nil {
			t.Fatalf("trusted %+v", c)
		}
	}
	key, err := st.TrustOffered(unk.Token, "10.0.0.5:22", fp, false)
	if err != nil || key.KeyType != "ssh-ed25519" {
		t.Fatalf("trust = %+v, %v", key, err)
	}
	// Used up.
	if _, err := st.TrustOffered(unk.Token, "10.0.0.5:22", fp, false); err == nil {
		t.Fatal("offer used twice")
	}

	// A changed key needs replace, and the saved key must not have moved.
	var mm *HostKeyMismatchError
	if err := st.CheckHostKeyType("10.0.0.5:22", "ssh-rsa", fp2); !errors.As(err, &mm) {
		t.Fatalf("err = %v", err)
	}
	if _, err := st.TrustOffered(mm.Token, "10.0.0.5:22", fp2, false); err == nil {
		t.Fatal("changed key trusted without replace")
	}
	if _, err := st.TrustOffered(mm.Token, "10.0.0.5:22", fp2, true); err != nil {
		t.Fatal(err)
	}
	if k, _ := st.KnownHost("10.0.0.5:22"); k.Fingerprint != fp2 || k.KeyType != "ssh-rsa" {
		t.Fatalf("known = %+v", k)
	}

	// Offers expire.
	if err := st.CheckHostKeyType("10.0.0.7:22", "ssh-ed25519", fp); !errors.As(err, &unk) {
		t.Fatal(err)
	}
	defer func(f func() time.Time) { offerNow = f }(offerNow)
	later := time.Now().Add(OfferTTL + time.Second)
	offerNow = func() time.Time { return later }
	if _, err := st.TrustOffered(unk.Token, "10.0.0.7:22", fp, false); !errors.Is(err, ErrNoOffer) {
		t.Fatalf("expired offer: %v", err)
	}
}

func TestHostKeyFormats(t *testing.T) {
	for _, ok := range []string{"10.0.0.5:22", "[fd00::1]:2222", "host.example:65535"} {
		if !ValidHostKeyName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "host", "host:0", "host:99999", ":22", "a b:22", "u@h:22", "fd00::1:22"} {
		if ValidHostKeyName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !ValidFingerprint("SHA256:q3VtXz0b6kP1d8yRk2mJw4sTn7aEc9hLf5uGi3o9fKe") || ValidFingerprint("SHA256:abc") || ValidFingerprint("MD5:aa") {
		t.Fatal("fingerprint format")
	}
}

func TestStartupCommandValidation(t *testing.T) {
	st := testStore(t)
	h := mustHost(t, st, Host{Name: "box", Address: "10.0.0.2", StartupCommand: "tmux new -A -s main"})
	if h.StartupCommand != "tmux new -A -s main" {
		t.Fatalf("startup = %q", h.StartupCommand)
	}
	for _, bad := range []string{"ls\nrm -rf /", "ls\r", "a\tb", "\x1b[A", strings.Repeat("x", MaxStartupCommand+1)} {
		h.StartupCommand = bad
		if _, err := st.UpsertHost(h); err == nil || !errors.Is(err, ErrHostField) {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
}

func TestKnownHostReadAndForget(t *testing.T) {
	st := testStore(t)
	h := Host{Address: "10.0.0.5", Port: 2222}
	name := HostKeyName(h)
	if name != "10.0.0.5:2222" {
		t.Fatalf("name = %q", name)
	}
	k, err := st.KnownHost(name)
	if err != nil || k.Known {
		t.Fatalf("known = %+v, %v", k, err)
	}
	if err := st.TrustHostKeyType(name, "SHA256:abc", "ssh-ed25519", false); err != nil {
		t.Fatal(err)
	}
	k, _ = st.KnownHost(name)
	if !k.Known || k.Fingerprint != "SHA256:abc" || k.KeyType != "ssh-ed25519" {
		t.Fatalf("known = %+v", k)
	}
	// Re-trusting the same key without a type keeps the type.
	if err := st.TrustHostKey(name, "SHA256:abc", false); err != nil {
		t.Fatal(err)
	}
	if k, _ = st.KnownHost(name); k.KeyType != "ssh-ed25519" {
		t.Fatalf("type lost: %+v", k)
	}
	// The type survives a reload.
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if k, _ = st2.KnownHost(name); k.KeyType != "ssh-ed25519" {
		t.Fatalf("type not persisted: %+v", k)
	}
	ok, err := st.ForgetHostKey(name)
	if err != nil || !ok {
		t.Fatalf("forget = %v, %v", ok, err)
	}
	var unk *UnknownHostError
	if err := st.CheckHostKey(name, "SHA256:abc"); !errors.As(err, &unk) {
		t.Fatalf("after forget: %v", err)
	}
	if ok, _ := st.ForgetHostKey(name); ok {
		t.Fatal("forgot twice")
	}
}
