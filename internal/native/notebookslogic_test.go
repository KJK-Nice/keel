package native

import (
	"slices"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// Test ports of the cases of cmd/keel/web/js/test/notebooks.test.mjs (placeholders, diffLines, history rows) and
// test/autosave.test.mjs that the native client uses.

func TestDiffLinesAlignsTheLinesThatStayedAndMarksTheRest(t *testing.T) {
	ops := func(d []diffLine) (out []string) {
		for _, l := range d {
			out = append(out, l.Op+":"+l.Text)
		}
		return out
	}
	d := diffLines("a\nb\nc\nd", "a\nB\nc\nd\ne", diffMaxCells)
	if got := ops(d); !slices.Equal(got, []string{"same:a", "del:b", "add:B", "same:c", "same:d", "add:e"}) {
		t.Fatalf("diff %v", got)
	}
	if got := diffSummary(d); got != "2 lines added · 1 removed" {
		t.Fatal(got)
	}
	if got := diffSummary(diffLines("x", "x", diffMaxCells)); got != "No changes" {
		t.Fatal(got)
	}
	if got := diffSummary(diffLines("x", "x\ny", diffMaxCells)); got != "1 line added · 0 removed" {
		t.Fatal(got)
	}
	// Too large to align: all of a removed, all of b added.
	big := diffLines("1\n2\n3", "4\n5\n6", 4)
	if got := ops(big); !slices.Equal(got, []string{"del:1", "del:2", "del:3", "add:4", "add:5", "add:6"}) {
		t.Fatalf("big %v", got)
	}
	v := diffView(diffLines("1\n2\n3\n4\n5\n6\n7\n8", "1\n2\n3\n4\n5\n6\n7\nX", diffMaxCells), 1)
	var vo []string
	for _, r := range v {
		vo = append(vo, r.Op)
	}
	if !slices.Equal(vo, []string{"gap", "same", "del", "add"}) || v[0].Count != 6 {
		t.Fatalf("view %v", v)
	}
}

func TestHistoryRowsSayWhoAndWhat(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	l := versionLabel(keelclient.NotebookVersion{By: "user", Event: "before-agent-edit", Summary: "Adds a step", At: now.Add(-2 * time.Minute)}, now)
	if got := []string{l.Who, l.What, l.When, l.Summary}; !slices.Equal(got, []string{"You", "Before the agent's edit", "2 min ago", "Then: Adds a step"}) {
		t.Fatalf("%q", got)
	}
	if versionLabel(keelclient.NotebookVersion{By: "agent", Event: "agent-version"}, now).Who != "Agent" {
		t.Fatal("agent version is not Agent's")
	}
	if got := versionLabel(keelclient.NotebookVersion{Event: "x"}, now).What; got != "Earlier version" {
		t.Fatal(got)
	}
	if got := versionText(keelclient.NotebookVersion{Cells: []keelclient.NotebookCell{{Body: "a"}, {Body: "b"}}}); got != "a\n\nb" {
		t.Fatal(got)
	}
	if privateLabel(keelclient.Notebook{Private: true}) != "Private" || privateLabel(keelclient.Notebook{}) != "Agent can read" {
		t.Fatal("privateLabel")
	}
	if got := whenText(now.Add(-10*time.Second), now); got != "just now" {
		t.Fatal(got)
	}
	if got := whenText(time.Time{}, now); got != "" {
		t.Fatal(got)
	}
}

func TestNotebooksSortPinnedThenNewestThenNameAndPreviewTheFirstLine(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	nbs := []keelclient.Notebook{
		{ID: "old", Name: "Old", UpdatedAt: t0},
		{ID: "b", Name: "beta", UpdatedAt: t0.Add(time.Hour)},
		{ID: "a", Name: "Alpha", UpdatedAt: t0.Add(time.Hour)},
		{ID: "pin", Name: "Pinned", Pinned: true, UpdatedAt: t0.Add(-time.Hour)},
	}
	var ids []string
	for _, n := range sortedNotebooks(nbs) {
		ids = append(ids, n.ID)
	}
	if !slices.Equal(ids, []string{"pin", "a", "b", "old"}) {
		t.Fatalf("order %v", ids)
	}
	if nbs[0].ID != "old" {
		t.Fatal("the sort changed its input")
	}
	nb := keelclient.Notebook{Cells: []keelclient.NotebookCell{{Body: "  \n"}, {Body: "\n  first line  \nsecond"}}}
	if got := notebookPreview(nb); got != "first line" {
		t.Fatal(got)
	}
	if got := notebookPreview(keelclient.Notebook{}); got != "Empty notebook" {
		t.Fatal(got)
	}
	if notebookName(keelclient.Notebook{}) != "Notebook" || notebookName(keelclient.Notebook{Name: "X"}) != "X" {
		t.Fatal("notebookName")
	}
}

func TestFencesAreReadFromTheMarkdownEachOneItsOwn(t *testing.T) {
	type want struct {
		lang, code string
		shell      bool
	}
	for name, c := range map[string]struct {
		md   string
		want []want
	}{
		"adjacent fences stay separate": {"```sh\nls\n```\n```bash\ndf -h\n```\n```go\nx := 1\n```", []want{{"sh", "ls", true}, {"bash", "df -h", true}, {"go", "x := 1", false}}},
		"one blank line between":        {"```sh\na\n```\n\n```sh\nb\n```", []want{{"sh", "a", true}, {"sh", "b", true}}},
		"tilde fences":                  {"~~~sh\nls -l\n~~~\n\n~~~\nplain\n~~~", []want{{"sh", "ls -l", true}, {"", "plain", false}}},
		"in a quote":                    {"> ```sh\n> uptime\n> ```", []want{{"sh", "uptime", true}}},
		"in a list item":                {"- step\n\n  ```bash\n  free -m\n  ```\n- next", []want{{"bash", "free -m", true}}},
		"a longer fence around ticks":   {"````sh\necho ```\n````", []want{{"sh", "echo ```", true}}},
		"extra words in the info":       {"```SHELL {title=\"x\"}\nwhoami\n```", []want{{"shell", "whoami", true}}},
		"unclosed at the end":           {"text\n\n```sh\nls\npwd", []want{{"sh", "ls\npwd", true}}},
		"an empty fence":                {"```sh\n```", []want{{"sh", "", true}}},
		"no fences":                     {"# Title\n\nSome `code` here", nil},
		"indented code is not a shell":  {"para\n\n    ls -l\n", []want{{"", "ls -l", false}}},
	} {
		got := fences(c.md)
		if len(got) != len(c.want) {
			t.Errorf("%s: %d fences %+v, want %d", name, len(got), got, len(c.want))
			continue
		}
		for i, w := range c.want {
			if got[i].Lang != w.lang || got[i].Code != w.code || got[i].Shell != w.shell {
				t.Errorf("%s: fence %d = %+v, want %+v", name, i, got[i], w)
			}
		}
	}
}

func TestDirectableIsOneShellLineWithNoPlaceholder(t *testing.T) {
	for md, want := range map[string]bool{
		"```sh\ndf -h\n```":               true,
		"```bash\ndf -h\n```":             true,
		"```shell\ndf -h\n```":            true,
		"```go\ndf -h\n```":               false,
		"```\ndf -h\n```":                 false,
		"```sh\ndf -h\nuptime\n```":       false,
		"```sh\n```":                      false,
		"```sh\nexport T=[secret:1]\n```": false,
		"```sh\nls [abc]\n```":            true,
	} {
		fs := fences(md)
		if len(fs) != 1 || directable(fs[0]) != want {
			t.Errorf("%q: directable = %v, want %v", md, len(fs) == 1 && directable(fs[0]), want)
		}
	}
}

// at the same index the document gives markdown.View's code block, so the Type button of a block types that block's code.
func TestFenceIndexIsTheBlockIndex(t *testing.T) {
	fs := fences("# t\n\n```sh\na\n```\n```sh\nb\n```")
	if len(fs) != 2 || fs[0].Index == fs[1].Index {
		t.Fatalf("%+v", fs)
	}
	if f, ok := fenceAt(fs, fs[1].Index); !ok || f.Code != "b" {
		t.Fatal(f, ok)
	}
	if _, ok := fenceAt(fs, -1); ok {
		t.Fatal("found a fence that is not there")
	}
}

// ---- autosave: test/autosave.test.mjs ----

// saver drives an autosave with a clock, the way the frame does: due at the time, then begin and end.
type saver struct {
	a     *autosave
	now   time.Time
	saves int
	slow  bool // a save stays running until finish
}

func newSaver(slow bool) *saver { return &saver{a: newAutosave(), now: time.Unix(1000, 0), slow: slow} }

// tick starts a save when one is due, and finishes it at once unless slow.
func (s *saver) tick() {
	for s.a.due(s.now) && s.a.begin() {
		s.saves++
		if !s.slow {
			s.a.end()
		}
	}
}

func (s *saver) advance(d time.Duration) {
	end := s.now.Add(d)
	for step := time.Millisecond * 50; s.now.Before(end); {
		s.now = s.now.Add(step)
		s.tick()
	}
}

func (s *saver) finish() {
	if s.a.end() && s.a.begin() {
		s.saves++
	}
}

func TestEditsSettleIntoOneSaveAfterTheIdleDelay(t *testing.T) {
	s := newSaver(false)
	for i := 0; i < 10; i++ {
		s.a.edit(s.now)
		s.advance(150 * time.Millisecond)
	}
	if s.saves != 0 {
		t.Fatalf("%d saves while typing", s.saves)
	}
	s.advance(1000 * time.Millisecond)
	if s.saves != 1 || s.a.pending() {
		t.Fatalf("saves %d pending %v", s.saves, s.a.pending())
	}
}

func TestALongBurstStillSavesEveryMaxWait(t *testing.T) {
	s := newSaver(false)
	for i := 0; i < 100; i++ {
		s.a.edit(s.now)
		s.advance(100 * time.Millisecond) // 10 s of typing, never idle for 1 s
	}
	if s.saves != 2 {
		t.Fatalf("%d saves in a 10 s burst, want 2 (at 5 s and 10 s)", s.saves)
	}
	s.advance(10 * time.Second)
	if s.saves != 2 || s.a.pending() { // the last edit (9.9 s) went out with the 10 s save
		t.Fatalf("saves %d pending %v", s.saves, s.a.pending())
	}
}

func TestFlushSavesAPendingEditNowAndNothingWhenClean(t *testing.T) {
	a := newAutosave()
	if a.begin() {
		t.Fatal("a clean notebook began a save")
	}
	a.edit(time.Unix(1, 0))
	if !a.begin() {
		t.Fatal("flush found nothing to save")
	}
	a.end()
	if a.pending() {
		t.Fatal("still pending")
	}
}

func TestOneSaveAtATimeAnEditDuringASaveRunsOneMoreAfterIt(t *testing.T) {
	s := newSaver(true)
	s.a.edit(s.now)
	s.advance(time.Second)
	if s.saves != 1 {
		t.Fatalf("saves %d", s.saves)
	}
	s.a.edit(s.now)
	s.advance(time.Second) // due, but the first save is still running
	if s.saves != 1 || !s.a.pending() {
		t.Fatalf("saves %d pending %v", s.saves, s.a.pending())
	}
	s.finish()
	if s.saves != 2 {
		t.Fatalf("the edit made during the save did not run after it: %d saves", s.saves)
	}
	s.finish()
	if s.a.pending() || s.saves != 2 {
		t.Fatalf("saves %d pending %v", s.saves, s.a.pending())
	}
}

func TestAFailedSaveDoesNotStopLaterOnes(t *testing.T) {
	a := newAutosave()
	calls := 0
	for i := 0; i < 2; i++ {
		a.edit(time.Unix(int64(i), 0))
		if a.begin() {
			calls++ // the save failed; its caller reports it, and the autosave carries on
			a.end()
		}
	}
	if calls != 2 {
		t.Fatalf("%d saves", calls)
	}
}

func TestWakeIsTheTimeUntilTheNextSave(t *testing.T) {
	a := newAutosave()
	now := time.Unix(100, 0)
	if a.wake(now) != 0 {
		t.Fatal("a clean notebook wants a wake")
	}
	a.edit(now)
	if got := a.wake(now); got != autosaveIdle {
		t.Fatalf("wake %v", got)
	}
	// 4.5 s into a burst the max (0.5 s away) comes before the idle time (1 s).
	a.edit(now.Add(4500 * time.Millisecond))
	if got := a.wake(now.Add(4500 * time.Millisecond)); got != 500*time.Millisecond {
		t.Fatalf("wake %v", got)
	}
}
