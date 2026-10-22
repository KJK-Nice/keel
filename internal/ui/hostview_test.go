package ui

import (
	"reflect"
	"testing"

	"keel/internal/store"
)

func TestHostFromKeepsWhatViewsShow(t *testing.T) {
	off := false
	got := HostFrom(store.Host{
		ID: "h", Name: "api", Group: "g", GroupID: "g", Address: "10.0.0.1", Port: 2222,
		Username: "ops", AuthType: "identity", IdentityID: "k", JumpHost: "j", Color: "c",
		Tags: []string{"prod"}, AgentReadable: &off,
		Password: "x", PrivateKey: "x", Passphrase: "x",
	})
	want := Host{
		ID: "h", Name: "api", Group: "g", GroupID: "g", Address: "10.0.0.1", Port: 2222,
		Username: "ops", AuthType: "identity", IdentityID: "k", JumpHost: "j", Color: "c",
		Tags: []string{"prod"}, AgentOff: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !HostFrom(store.Host{}).ReadableByAgent() {
		t.Fatal("a host that never set it is readable")
	}
}

// The view type must never grow a secret field.
func TestHostViewHasNoSecretFields(t *testing.T) {
	ty := reflect.TypeOf(Host{})
	for _, name := range []string{"Password", "PrivateKey", "Passphrase"} {
		if _, ok := ty.FieldByName(name); ok {
			t.Errorf("ui.Host has %s", name)
		}
	}
}
