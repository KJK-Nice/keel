package store

import (
	"errors"
	"os"
	"strings"
	"testing"
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

func TestResolveDraftUsesSavedSecrets(t *testing.T) {
	st := testStore(t)
	bastion := mustHost(t, st, Host{Name: "bastion", Address: "10.0.0.1", Password: "jump-pw"})
	db := mustHost(t, st, Host{Name: "db", Address: "10.0.4.21", Password: "saved-pw"})

	// Unsaved edits: new address, jump host, password left blank.
	h, err := st.ResolveDraft(Host{ID: db.ID, Name: "db", Address: "10.0.4.22", Username: "u", AuthType: "password", Password: "__saved__", JumpHost: bastion.ID})
	if err != nil {
		t.Fatal(err)
	}
	if h.Password != "saved-pw" || h.Address != "10.0.4.22" || h.Jump == nil || h.Jump.Password != "jump-pw" {
		t.Fatalf("draft = %+v jump=%+v", h, h.Jump)
	}
	// A typed password wins; nothing is saved.
	h, err = st.ResolveDraft(Host{ID: db.ID, Address: "10.0.4.21", Username: "u", AuthType: "password", Password: "typed"})
	if err != nil || h.Password != "typed" {
		t.Fatalf("draft = %+v, %v", h, err)
	}
	saved, _ := st.GetHost(db.ID)
	if saved.Password != "saved-pw" || saved.Address != "10.0.4.21" || saved.JumpHost != "" {
		t.Fatalf("ResolveDraft saved something: %+v", saved)
	}
	// Self reference is rejected for drafts too.
	if _, err := st.ResolveDraft(Host{ID: db.ID, Address: "x", Username: "u", AuthType: "password", JumpHost: db.ID}); err == nil {
		t.Fatal("self jump accepted")
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
