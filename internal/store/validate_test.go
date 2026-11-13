package store

import (
	"errors"
	"testing"
)

// Every save of a host or an identity is checked by the store, whoever
// calls it: the API, the ssh config import, a duplicate.
func TestUpsertHostChecksNameAndSignIn(t *testing.T) {
	st := testStore(t)
	ident, err := st.UpsertIdentity(Identity{Name: "deploy", Kind: "key", Username: "ubuntu", PrivateKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	nameless, err := st.UpsertIdentity(Identity{Name: "no user", Kind: "key", PrivateKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListHosts()
	for _, c := range []struct {
		h    Host
		want string
	}{
		{Host{Address: "a", Username: "u", AuthType: "password"}, "name is required"},
		{Host{Name: "  ", Address: "a", Username: "u", AuthType: "password"}, "name is required"},
		{Host{Name: "n", Address: "a", Username: "u"}, "authType must be local, password, key, or identity"},
		{Host{Name: "n", Address: "a", Username: "u", AuthType: "telnet"}, "authType must be local, password, key, or identity"},
		{Host{Name: "n", Username: "u", AuthType: "password"}, "address is required"},
		{Host{Name: "n", Address: "a", AuthType: "key"}, "username is required"},
		{Host{Name: "n", Address: "a", Username: "u", AuthType: "identity"}, "identity is required"},
		{Host{Name: "n", Address: "a", AuthType: "identity", IdentityID: nameless.ID}, "username is required"},
		{Host{Name: "n", Address: "a", AuthType: "identity", IdentityID: "gone"}, "username is required"},
	} {
		_, err := st.UpsertHost(c.h)
		if !errors.Is(err, ErrHostField) || err.Error() != c.want {
			t.Errorf("UpsertHost(%+v) = %v, want ErrHostField %q", c.h, err, c.want)
		}
	}
	after, _ := st.ListHosts()
	if len(after) != len(before) {
		t.Fatalf("a refused host was saved: %d hosts, had %d", len(after), len(before))
	}
	for _, ok := range []Host{
		{Name: "shell", AuthType: AuthLocal},
		{Name: "pw", Address: "a", Username: "u", AuthType: "password"},
		{Name: "by identity", Address: "a", AuthType: "identity", IdentityID: ident.ID},
	} {
		if _, err := st.UpsertHost(ok); err != nil {
			t.Errorf("UpsertHost(%+v) = %v", ok, err)
		}
	}
}

func TestUpsertIdentityChecksNameAndKind(t *testing.T) {
	st := testStore(t)
	for _, c := range []struct {
		in   Identity
		want string
	}{
		{Identity{Kind: "key"}, "name is required"},
		{Identity{Name: " ", Kind: "password"}, "name is required"},
		{Identity{Name: "n"}, "kind must be password or key"},
		{Identity{Name: "n", Kind: "local"}, "kind must be password or key"},
	} {
		_, err := st.UpsertIdentity(c.in)
		if !errors.Is(err, ErrIdentityField) || err.Error() != c.want {
			t.Errorf("UpsertIdentity(%+v) = %v, want ErrIdentityField %q", c.in, err, c.want)
		}
	}
	if ids, _ := st.ListIdentities(); len(ids) != 0 {
		t.Fatalf("a refused identity was saved: %+v", ids)
	}
}

func TestDuplicateOfTheLocalShellIsATypedRefusal(t *testing.T) {
	st := testStore(t)
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		if !h.IsLocal() {
			continue
		}
		if _, err := st.DuplicateHost(h.ID); !errors.Is(err, ErrLocalDuplicate) {
			t.Fatalf("DuplicateHost(local shell) = %v, want ErrLocalDuplicate", err)
		}
		return
	}
	t.Fatal("the seeded store has no local shell")
}

// A list shows SavedSecret where a secret is held, and saving that host
// back keeps the secret: the marker is never stored as the secret.
func TestListedHostSavedBackKeepsItsSecret(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{Name: "pw", Address: "a", Username: "u", AuthType: "password", Password: "real-secret"})
	if err != nil {
		t.Fatal(err)
	}
	hosts, _ := st.ListHosts()
	for _, listed := range hosts {
		if listed.ID != h.ID {
			continue
		}
		if listed.Password != SavedSecret || listed.PrivateKey != "" {
			t.Fatalf("listed secrets = %q, %q", listed.Password, listed.PrivateKey)
		}
		if _, err := st.UpsertHost(listed); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetHost(h.ID)
	if err != nil || got.Password != "real-secret" {
		t.Fatalf("after saving the listed host back: %q, %v", got.Password, err)
	}
}
