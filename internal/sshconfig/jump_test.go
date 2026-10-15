package sshconfig

import "testing"

func TestParseJump(t *testing.T) {
	ok := map[string]Jump{
		"bastion":            {Host: "bastion"},
		"ops@bastion":        {User: "ops", Host: "bastion"},
		"ops@bastion:2222":   {User: "ops", Host: "bastion", Port: 2222},
		"[fd00::1]:2200":     {Host: "fd00::1", Port: 2200},
		"fd00::1":            {Host: "fd00::1"},
		"a@b@host":           {User: "a@b", Host: "host"},
		" gate.example.net ": {Host: "gate.example.net"},
	}
	for in, want := range ok {
		got, good := ParseJump(in)
		if !good || got != want {
			t.Errorf("ParseJump(%q) = %+v, %v; want %+v", in, got, good, want)
		}
	}
	for _, in := range []string{"", "a,b", "ssh://h", "h:0", "h:x", "@h", "[fd00::1", "[fd00::1]x"} {
		if _, good := ParseJump(in); good {
			t.Errorf("ParseJump(%q) accepted", in)
		}
	}
}
