package providertest

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
)

// ScriptedJudge is the test detector `keel serve` uses with
// KEEL_AGENT_PROVIDER=fake, in place of TypeSafe Jev. Like Scripted it
// sends nothing anywhere: it answers in this process from simple rules,
// and keeps the last few requests for Go tests.
//
//   - A Noul about the prompt line is high (0.95) when the line reads like
//     words: two or more, the first not a common command, no shell
//     operators or flags. Otherwise it is low (0.05).
//   - A line holding "jev-offline" makes the whole request fail, so a
//     test can see the silent fallback to #-lines.
//   - A Choice about a command answers "readonly", except "changes" for a
//     command holding "keel-jev-stricter", so a test can see Jev make a
//     rating stricter, and "destructive" after SlowDelay for a command
//     holding "keel-jev-slow", so a test can see the card arrive first
//     and its rating tighten later.
//   - A history pick (a `candidates` map in the state) picks the candidate
//     that holds "keel-" and the request's words joined by "-" (the words
//     "history marker" pick `echo keel-history-marker`), sure of it (0.95
//     everywhere); for words with "unsure" it picks the first candidate
//     at 0.40, below every bar; otherwise none_of_the_above.
type ScriptedJudge struct {
	mu       sync.Mutex
	requests []provider.JudgeRequest
	slow     time.Duration
}

// DefaultSlowDelay is how long the fake takes over a "keel-jev-slow"
// command: well under the service's risk timeout (2 s), long enough for
// an end-to-end test to see the card before the answer.
const DefaultSlowDelay = 1200 * time.Millisecond

func NewScriptedJudge() *ScriptedJudge { return &ScriptedJudge{slow: DefaultSlowDelay} }

// SetSlowDelay changes the "keel-jev-slow" delay (Go tests shorten it).
func (j *ScriptedJudge) SetSlowDelay(d time.Duration) {
	j.mu.Lock()
	j.slow = d
	j.mu.Unlock()
}

func (j *ScriptedJudge) ID() string { return "fake-judge" }

var errOffline = errors.New("fake judge: offline")

var commandWords = map[string]bool{
	"ls": true, "cd": true, "cat": true, "echo": true, "git": true, "sudo": true, "rm": true, "cp": true, "mv": true,
	"grep": true, "find": true, "ps": true, "top": true, "df": true, "du": true, "make": true, "go": true, "npm": true,
	"docker": true, "kubectl": true, "ssh": true, "vim": true, "vi": true, "less": true, "tail": true, "head": true,
	"printf": true, "export": true, "systemctl": true, "journalctl": true, "curl": true, "touch": true, "mkdir": true,
}

// LooksPlain is the fake's plain-language rule.
func LooksPlain(line string) bool {
	f := strings.Fields(line)
	if len(f) < 2 || commandWords[strings.ToLower(f[0])] {
		return false
	}
	if strings.ContainsAny(line, "|;&$<>=`/\\") {
		return false
	}
	for _, w := range f {
		if strings.HasPrefix(w, "-") {
			return false
		}
	}
	return true
}

func (j *ScriptedJudge) Judge(ctx context.Context, req provider.JudgeRequest) (provider.JudgeResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.JudgeResponse{}, err
	}
	j.mu.Lock()
	j.requests = append(j.requests, req)
	if len(j.requests) > keepRequests {
		j.requests = j.requests[len(j.requests)-keepRequests:]
	}
	j.mu.Unlock()

	line := ""
	for _, v := range req.Fields {
		line += v.String()
	}
	if strings.Contains(line, "jev-offline") {
		return provider.JudgeResponse{}, errOffline
	}
	out := provider.JudgeResponse{Model: "fake-judge", Nouls: map[string]float64{}, Choices: map[string]provider.ChoiceAnswer{}}
	if cands, ok := req.Maps["candidates"]; ok {
		return historyPick(req, line, cands, out), nil
	}
	cmds := map[int]string{}
	slow := false
	for _, list := range req.Lists {
		for i, v := range list {
			cmds[i] = v.String()
			slow = slow || strings.Contains(v.String(), "keel-jev-slow")
		}
	}
	if slow {
		j.mu.Lock()
		d := j.slow
		j.mu.Unlock()
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return provider.JudgeResponse{}, ctx.Err()
		}
	}
	for id, q := range req.Questions {
		switch q.Type {
		case provider.QuestionNoul:
			p := 0.05
			if LooksPlain(line) {
				p = 0.95
			}
			out.Nouls[id] = p
		case provider.QuestionChoice:
			// risk_N names commands[N].
			n := 0
			if i := strings.LastIndexByte(id, '_'); i >= 0 {
				for _, c := range id[i+1:] {
					n = n*10 + int(c-'0')
				}
			}
			pick := "readonly"
			switch {
			case strings.Contains(cmds[n], "keel-jev-stricter"):
				pick = "changes"
			case strings.Contains(cmds[n], "keel-jev-slow"):
				pick = "destructive"
			}
			probs := map[string]float64{}
			for _, o := range q.Options {
				probs[o.Key] = 0
			}
			probs[pick] = 1
			out.Choices[id] = provider.ChoiceAnswer{Choice: pick, Probabilities: probs}
		}
	}
	return out, nil
}

// historyPick answers a history request (see ScriptedJudge).
func historyPick(req provider.JudgeRequest, words string, cands map[string]redact.Safe, out provider.JudgeResponse) provider.JudgeResponse {
	words = strings.ToLower(strings.TrimSpace(strings.TrimLeft(words, "#")))
	want := "keel-" + strings.Join(strings.Fields(words), "-")
	pick, p := "", 0.0
	keys := make([]string, 0, len(cands))
	for k := range cands {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		return len(keys[a]) < len(keys[b]) || (len(keys[a]) == len(keys[b]) && keys[a] < keys[b])
	})
	for _, k := range keys {
		if strings.Contains(cands[k].String(), want) {
			pick, p = k, 0.95
			break
		}
	}
	if pick == "" && strings.Contains(words, "unsure") && len(keys) > 0 {
		pick, p = keys[0], 0.40
	}
	for id, q := range req.Questions {
		switch {
		case q.Type == provider.QuestionChoice:
			probs := map[string]float64{}
			for _, o := range q.Options {
				probs[o.Key] = 0
			}
			choice := "none_of_the_above"
			if pick != "" {
				choice = pick
			}
			probs[choice] = 1
			if pick != "" {
				probs[choice] = p
				probs["none_of_the_above"] = 1 - p
			}
			out.Choices[id] = provider.ChoiceAnswer{Choice: choice, Probabilities: probs}
		case id == "exists":
			out.Nouls[id] = max(p, 0.05)
		case strings.HasPrefix(id, "fit_"):
			v := 0.05
			if strings.TrimPrefix(id, "fit_") == pick {
				v = p
			}
			out.Nouls[id] = v
		}
	}
	return out
}

// Requests returns the last requests Judge received, oldest first.
func (j *ScriptedJudge) Requests() []provider.JudgeRequest {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]provider.JudgeRequest(nil), j.requests...)
}

// FakeJudge is a judge with fixed answers for unit tests.
type FakeJudge struct {
	mu      sync.Mutex
	Nouls   map[string]float64
	Choices map[string]provider.ChoiceAnswer
	Err     error
	// Hold, when set, keeps every answer back until it is closed (or the
	// request's context ends), so a test can act while Jev is "thinking".
	Hold     chan struct{}
	requests []provider.JudgeRequest
}

func (f *FakeJudge) ID() string { return "fake-judge" }

func (f *FakeJudge) Judge(ctx context.Context, req provider.JudgeRequest) (provider.JudgeResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.JudgeResponse{}, err
	}
	f.mu.Lock()
	hold := f.Hold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return provider.JudgeResponse{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.Err != nil {
		return provider.JudgeResponse{}, f.Err
	}
	out := provider.JudgeResponse{Model: "fake-judge", Nouls: map[string]float64{}, Choices: map[string]provider.ChoiceAnswer{}}
	for id := range req.Questions {
		if v, ok := f.Nouls[id]; ok {
			out.Nouls[id] = v
		}
		if v, ok := f.Choices[id]; ok {
			out.Choices[id] = v
		}
	}
	return out, nil
}

// Requests returns what Judge received, in order.
func (f *FakeJudge) Requests() []provider.JudgeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.JudgeRequest(nil), f.requests...)
}

var (
	_ provider.Judge = (*ScriptedJudge)(nil)
	_ provider.Judge = (*FakeJudge)(nil)
)
