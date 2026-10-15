package service

import (
	"fmt"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
)

// The questions Keel asks the detector (TypeSafe Jev) and the numbers it
// acts on, in one place so they can be reviewed and tuned together
// (docs/ui-redesign.md §5.5, §7). The client's debounce is in
// cmd/keel/web/js/lib/suggest.js (DETECT_PAUSE_MS).
//
// Tuned against jev-1.13.0 (provider/jev.Model) on 2026-10-15 with 14
// prompt lines and 6 commands: plain requests scored 0.80–0.98 ("list
// docker containers" 0.80, "which process is listening on 5432" 0.98),
// commands 0.01–0.28 ("git status" 0.28 in a batch, 0.02 alone). The
// round trip from a home connection was 270–470 ms, so the < 150 ms hint
// budget of §7 holds only near the endpoint; the timeout below keeps a
// slow answer from showing a stale hint.

// Untrusted terminal text goes in these state fields, never inside a
// question. Questions point at them by path.
const (
	statePromptLine = "prompt_line" // the words on the prompt line, redacted
	stateCommands   = "commands"    // candidate commands, redacted
	stateRequest    = "request"     // the words on the prompt line, redacted (history pick)
	stateCandidates = "candidates"  // commands already run or curated, keyed c0…cN (history pick)
)

const (
	// NoticeThreshold: P(plain language) at or above which the prompt line
	// is noticed (State 1). High on purpose: a missed notice costs
	// nothing (⇥ keeps its shell meaning), a false one steals ⇥.
	NoticeThreshold = 0.70
	// A Jev risk rating counts only when it is at least this sure. Jev
	// can only make Keel's code rating stricter (agent.Stricter).
	RiskDestructiveMin = 0.50 // P(destructive)
	RiskChangesMin     = 0.50 // P(changes) + P(destructive)

	// detectTimeout bounds one detection call. Past it the line is
	// treated as not noticed (only #-lines are), with no error shown.
	detectTimeout = 900 * time.Millisecond
	// riskTimeout bounds the Jev risk call after a suggestion. The card is
	// already shown by then (RiskCheck runs after it); past the timeout
	// the code rating stands alone.
	riskTimeout = 2 * time.Second
	// suggestTimeout bounds the model provider call for a suggestion.
	suggestTimeout = 30 * time.Second

	// detectLogWindow: detection reads of one pane are summarised into
	// one agent.read entry per window (the first is logged at once).
	detectLogWindow = time.Minute

	// minDetectWords: a line of one word is a command or a typo; it is
	// never sent.
	minDetectWords  = 2
	maxLineBytes    = 512
	suggestionTTL   = 10 * time.Minute
	maxSuggestions  = 64
	maxAlternatives = 2
)

// History first (phase 6c+): before the model writes a command, code
// gathers commands the user already ran in the pane (its scrollback) or
// marked agent-usable in Snippets, and Jev picks one in one request:
//
//   - pick: a Choice over the candidates by opaque key (c0…cN) plus
//     none_of_the_above. The keys name entries of the `candidates` map
//     in the state, never the text: Jev miscounted list indices past
//     about 15 entries when probed (`commands[18]` read as entry 17), and a
//     named key leaves nothing to count.
//   - exists: an absolute Noul, "does any candidate, as written, do what
//     was asked?". A Choice always has a winner; this says whether to act
//     at all.
//   - fit_cN: one Noul per candidate, "does `candidates.cN`, as written,
//     do what was asked?". The pick is not known when the request is
//     built, so every candidate gets one; code reads the picked one's.
//     They cost input tokens only (the state is sent once): 32 questions
//     over 30 candidates were 3.4k input tokens and 260–380 ms.
//
// Code acts only when all three agree (see historyConfident). Otherwise
// the request falls through to the model, as before.
//
// Probed on jev-1.13.0 on 2026-10-15 with 30 typical shell commands and 10
// requests. Right picks: P(pick) 0.73–1.00, fit 0.78–0.95, exists
// 0.75–0.96 ("print the history marker" was the weakest: 0.85 / 0.78 /
// 0.82). No fitting command ("compress the logs folder", "list pods in
// staging" against `kubectl get pods -n prod`): none_of_the_above 0.90–1.00,
// exists 0.04–0.06, every fit ≤ 0.05.
const (
	// HistoryPickMin: the Choice's probability of the picked candidate.
	HistoryPickMin = 0.50
	// HistoryFitMin: the picked candidate's own fit Noul. The bar that
	// matters most: a wrong history pick shows a command the user did
	// not ask for, where falling through only costs a model call.
	HistoryFitMin = 0.70
	// HistoryExistsMin: the absolute "any candidate fits" Noul.
	HistoryExistsMin = 0.60

	// historyTimeout bounds the pick. Past it the model is asked as
	// before, so a slow Jev costs at most this much on top.
	historyTimeout = 1500 * time.Millisecond
	// maxHistoryCandidates: at most this many candidates go to Jev, the
	// ones nearest the typed words (prefilter), then the most recent.
	maxHistoryCandidates = 40
	// maxHistoryCommandBytes: a longer command line is not offered.
	maxHistoryCommandBytes = 300
	// maxPromptBytes: a prompt (the text before $, #, %, >, ❯ or ») is at
	// most this long; a longer "prompt" is output that holds a $.
	maxPromptBytes = 64

	// historyNone is the Choice option for "no candidate does it".
	historyNone = "none_of_the_above"
)

func historyKey(i int) string { return fmt.Sprintf("c%d", i) }

func historyFitID(i int) string { return "fit_" + historyKey(i) }

// historyPickQuestion picks the candidate that does what was asked.
func historyPickQuestion(n int) provider.Question {
	opts := make([]provider.Option, 0, n+1)
	for i := 0; i < n; i++ {
		opts = append(opts, provider.Option{Key: historyKey(i), Description: "The command `" + stateCandidates + "." + historyKey(i) + "`"})
	}
	opts = append(opts, provider.Option{Key: historyNone, Description: "No command in `" + stateCandidates + "` does what `" + stateRequest + "` asks"})
	return provider.Question{
		Type:         provider.QuestionChoice,
		Instructions: "Which shell command in `" + stateCandidates + "`, run as it is written, does what `" + stateRequest + "` asks for?",
		Options:      opts,
	}
}

// historyExistsQuestion is the absolute check: is there a fit at all?
var historyExistsQuestion = provider.Question{
	Type:         provider.QuestionNoul,
	Instructions: "Does at least one shell command in `" + stateCandidates + "`, run exactly as written, do what `" + stateRequest + "` asks for?",
	True:         "One of the commands, unchanged, does what was asked",
	False:        "None does it, or each would need different arguments or another command",
}

// historyFitQuestion asks whether one candidate does what was asked.
func historyFitQuestion(i int) provider.Question {
	return provider.Question{
		Type:         provider.QuestionNoul,
		Instructions: "Does the shell command `" + stateCandidates + "." + historyKey(i) + "`, run exactly as written, do what `" + stateRequest + "` asks for?",
		True:         "Running it unchanged does what was asked",
		False:        "It does something else, or would need different arguments",
	}
}

// historyConfident reads the pick: the index of the candidate to offer,
// or -1 to fall through to the model. All three answers must clear their
// bar (the minimum, as for a call built from parts).
func historyConfident(resp provider.JudgeResponse, n int) int {
	pick, ok := resp.Choices["pick"]
	if !ok || pick.Choice == historyNone {
		return -1
	}
	i := -1
	for k := 0; k < n; k++ {
		if historyKey(k) == pick.Choice {
			i = k
		}
	}
	if i < 0 {
		return -1
	}
	fit, okFit := resp.Nouls[historyFitID(i)]
	exists, okExists := resp.Nouls["exists"]
	if !okFit || !okExists {
		return -1
	}
	if pick.Probabilities[pick.Choice] < HistoryPickMin || fit < HistoryFitMin || exists < HistoryExistsMin {
		return -1
	}
	return i
}

// detectQuestion is one Noul, phrased so that high = plain language.
var detectQuestion = provider.Question{
	Type:         provider.QuestionNoul,
	Instructions: "Is `" + statePromptLine + "` a request written in natural language words that says what the user wants done or wants to know, rather than a shell command to run?",
	True:         "A plain-language request or question, such as: which process is listening on 5432",
	False:        "A shell command line, command fragment, path or code, such as: ls -la /var/log",
}

// Risk options. Keys are agent.Risk values.
var riskOptions = []provider.Option{
	{Key: string(agent.RiskReadOnly), Description: "Only reads or shows information; changes no file, service, package, user or setting"},
	{Key: string(agent.RiskChanges), Description: "Changes state that can be put back: writes or moves files, edits config, installs packages, starts, stops or restarts a service"},
	{Key: string(agent.RiskDestructive), Description: "Deletes or overwrites data, or takes a host, disk, database or service down in a way that is hard to undo"},
}

func riskQuestionID(i int) string { return fmt.Sprintf("risk_%d", i) }

// riskQuestion asks for the worst effect of one candidate command.
func riskQuestion(i int) provider.Question {
	return provider.Question{
		Type:         provider.QuestionChoice,
		Instructions: fmt.Sprintf("If the shell command `%s[%d]` is run on a Linux server, what is the worst thing it does to that server?", stateCommands, i),
		Options:      riskOptions,
	}
}

// judgedRisk turns a risk answer into a rating, using the thresholds
// above. An answer below both is read-only, which Stricter then ignores.
func judgedRisk(a provider.ChoiceAnswer) agent.Risk {
	d := a.Probabilities[string(agent.RiskDestructive)]
	c := a.Probabilities[string(agent.RiskChanges)]
	switch {
	case d >= RiskDestructiveMin:
		return agent.RiskDestructive
	case c+d >= RiskChangesMin:
		return agent.RiskChanges
	}
	return agent.RiskReadOnly
}

// suggestPrompt is the model provider's system prompt for one in-pane
// suggestion. It reuses the panel's proposal block (ProposalFence).
const suggestPrompt = "You turn a request typed on a shell prompt line into a shell command, inside Keel, a local SSH workspace.\n" +
	"The user typed words instead of a command and asked for a suggestion. Values that look secret (and, if the user asked, " +
	"IP addresses and hostnames) were replaced with placeholders such as [secret:1], [ip:2] or [host:1]; never guess them.\n" +
	"You cannot run anything. The user reviews the command and inserts it on the prompt line; it is not run until they press Enter.\n" +
	"Reply with 1 to 3 commands, best first, each in its own fenced block in exactly this form and nothing else:\n" +
	"```" + ProposalFence + "\n" +
	"{\"command\": \"ss -ltnp 'sport = :5432'\", \"why\": \"Lists TCP sockets listening on port 5432 and the process that owns each one.\", \"risk\": \"readonly\"}\n" +
	"```\n" +
	"\"command\" is one shell command line: no prompt, no trailing newline, no comments. \"why\" is one short sentence. " +
	"\"risk\" is \"readonly\", \"changes\" or \"destructive\". Prefer read-only commands when they answer the request."

var suggestSafe = redact.Static(suggestPrompt)

// SuggestPrefix starts the user message of a suggestion request; the e2e
// fake provider (providertest.Scripted) recognises it.
const SuggestPrefix = "Words on the prompt line:\n"
