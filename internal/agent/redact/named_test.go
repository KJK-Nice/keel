package redact

import (
	"strings"
	"testing"
)

// Captured values and group variables are replaced by their {{name}}; a
// longer secret around one still wins, so nothing next to it is left out;
// short values are skipped like vault values; a bad name never draws text.
func TestNamedValues(t *testing.T) {
	r := New(Options{Named: []Named{
		{Name: "join_token", Value: "abcdef.0123456789abcdef", Kind: KindCaptured},
		{Name: "api_vip", Value: "10.20.0.10", Kind: KindVariable},
		{Name: "cri", Value: "crio", Kind: KindVariable},
		{Name: "tiny", Value: "ab", Kind: KindVariable},
		{Name: "Bad}}name", Value: "evilvalue123", Kind: KindCaptured},
	}})
	out := r.Text("kubeadm join 10.20.0.10:6443 --token abcdef.0123456789abcdef\nuse crio not criox; ab\nevilvalue123").String()
	for _, leak := range []string{"abcdef.0123456789abcdef", "10.20.0.10"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "join {{api_vip}}:6443 --token {{join_token}}") || !strings.Contains(out, "use {{cri}} not criox") || !strings.Contains(out, "; ab") {
		t.Fatalf("got %s", out)
	}
	if strings.Contains(out, "Bad}}name") {
		t.Fatal("a bad name was drawn")
	}
	rep := r.Report()
	if rep.Counts[KindCaptured] != 1 || rep.Counts[KindVariable] != 2 {
		t.Fatalf("report %+v", rep)
	}
	// A secret span longer than the captured value still covers it.
	r2 := New(Options{Named: []Named{{Name: "tok", Value: "S3cretPart", Kind: KindCaptured}}})
	out2 := r2.Text("password=S3cretPartAndMore").String()
	if strings.Contains(out2, "AndMore") || strings.Contains(out2, "S3cretPart") {
		t.Fatalf("got %s", out2)
	}
}

func TestHoldsKnown(t *testing.T) {
	r := New(Options{Known: []string{"vault-host-password-7f3c9e"}})
	if !r.HoldsKnown("password=vault-host-password-7f3c9e") || r.HoldsKnown("nothing") {
		t.Fatal("HoldsKnown wrong")
	}
	if r.Report().Total() != 0 {
		t.Fatal("HoldsKnown counted")
	}
}
