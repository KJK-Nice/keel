//go:build keelfake

package app

import "testing"

// go test -tags keelfake ./internal/app/
func TestOnlyFakeSelectsTheFake(t *testing.T) {
	for _, v := range []string{"", "gemini", "FAKE", "fake ", "http://example.com"} {
		if _, _, fake, err := agentProviders(v); fake || err != nil {
			t.Errorf("KEEL_AGENT_PROVIDER=%q: fake=%v err=%v", v, fake, err)
		}
	}
	fac, judge, fake, err := agentProviders("fake")
	if err != nil || !fake {
		t.Fatalf("fake not selected: %v", err)
	}
	p, err := fac("gemini", "any")
	if err != nil || p.ID() != "fake" {
		t.Fatalf("fake factory = %v, %v", p, err)
	}
	j, err := judge("any")
	if err != nil || j.ID() != "fake-judge" {
		t.Fatalf("fake judge = %v, %v", j, err)
	}
}
