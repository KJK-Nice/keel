package providertest

import (
	"context"
	"errors"
	"strings"
	"sync"

	"keel/internal/agent/provider"
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
//     rating stricter.
type ScriptedJudge struct {
	mu       sync.Mutex
	requests []provider.JudgeRequest
}

func NewScriptedJudge() *ScriptedJudge { return &ScriptedJudge{} }

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
	cmds := map[int]string{}
	for _, list := range req.Lists {
		for i, v := range list {
			cmds[i] = v.String()
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
			if strings.Contains(cmds[n], "keel-jev-stricter") {
				pick = "changes"
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

// Requests returns the last requests Judge received, oldest first.
func (j *ScriptedJudge) Requests() []provider.JudgeRequest {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]provider.JudgeRequest(nil), j.requests...)
}

// FakeJudge is a judge with fixed answers for unit tests.
type FakeJudge struct {
	mu       sync.Mutex
	Nouls    map[string]float64
	Choices  map[string]provider.ChoiceAnswer
	Err      error
	requests []provider.JudgeRequest
}

func (f *FakeJudge) ID() string { return "fake-judge" }

func (f *FakeJudge) Judge(ctx context.Context, req provider.JudgeRequest) (provider.JudgeResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.JudgeResponse{}, err
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
