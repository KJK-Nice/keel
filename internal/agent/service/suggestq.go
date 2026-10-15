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
