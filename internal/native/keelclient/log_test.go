package keelclient

import (
	"slices"
	"strconv"
	"testing"
)

// The connection log follows term.js recordFailStatus and lib/workspace.js logPane.
func TestTheConnectionLogKeepsStepsAndFailuresAndDropsNoise(t *testing.T) {
	var s State
	steps := []struct {
		prev, status, msg string
	}{
		{"", "connecting", "Connecting…"},
		{"connecting", "connecting", "  Connecting…  "}, // a repeat of the last line, whitespace folded
		{"connecting", "connecting", "Attached"},        // noise
		{"connecting", "connecting", "handshake timed out"},
		{"connecting", "error", "dial tcp 10.0.0.1:22: connection refused"},
		{"error", "error", ""}, // no message: nothing to add (the pane's headline says it)
	}
	for _, st := range steps {
		s.recordLog(st.prev, st.status, st.msg)
	}
	want := []LogLine{{"work", "Connecting…"}, {"fail", "handshake timed out"}, {"fail", "dial tcp 10.0.0.1:22: connection refused"}}
	if !slices.Equal(s.Log, want) {
		t.Fatalf("log %v, want %v", s.Log, want)
	}
	// A reconnect after a failure begins a new log; ready forgets it.
	s.recordLog("error", "connecting", "Reconnecting")
	if want := []LogLine{{"work", "Reconnecting"}}; !slices.Equal(s.Log, want) {
		t.Fatalf("after a reconnect: %v", s.Log)
	}
	s.recordLog("connecting", "ready", "")
	if s.Log != nil {
		t.Fatalf("a ready session keeps a log: %v", s.Log)
	}
	// A session that ended says so; presence and control frames (no status) add nothing.
	s.recordLog("ready", "exited", "Session ended")
	s.recordLog("exited", "", "took control")
	s.recordLog("exited", "", "ann joined the session")
	if want := []LogLine{{"fail", "Session ended"}}; !slices.Equal(s.Log, want) {
		t.Fatalf("after exit: %v", s.Log)
	}
}

func TestTheConnectionLogKeepsTheLastFortyAndNeverChangesAHandedOutCopy(t *testing.T) {
	var s State
	s.recordLog("", "connecting", "step 0")
	handed := s
	for i := 1; i < 60; i++ {
		s.recordLog("connecting", "connecting", "step "+strconv.Itoa(i))
	}
	if len(s.Log) != logMax || s.Log[0].Text != "step 20" || s.Log[logMax-1].Text != "step 59" {
		t.Fatalf("%d lines, first %q last %q", len(s.Log), s.Log[0].Text, s.Log[len(s.Log)-1].Text)
	}
	if len(handed.Log) != 1 || handed.Log[0].Text != "step 0" {
		t.Fatalf("a State handed out earlier changed: %v", handed.Log)
	}
}
