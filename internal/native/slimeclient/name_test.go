package slimeclient

import "testing"

// The name slime app passes (Client.Name) is the one a session socket carries, whatever the environment says.
func TestPassedNameWinsOverTheEnvironment(t *testing.T) {
	t.Setenv("SLIME_NAME", "from-env")
	f := newFake(t)
	f.c.Name = "passed"
	spawn(t, f)
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.query["name"]; len(got) != 1 || got[0] != "passed" {
		t.Fatalf("name = %v, want passed", got)
	}
}
