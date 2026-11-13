package native

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// The pure helpers of Settings · Agent (docs/ui-redesign.md §5.9): the words and small decisions of
// cmd/keel/web/js/lib/agentsettings.js, pinned there by test/agentsettings.test.mjs and here by agentsettingshelpers_test.go.
// A key never comes back from keel serve, so nothing here can show one: a provider is only ever configured or not.

// providerPurpose is what each provider is for, in one line: Keel's own wording (the descriptor carries the name,
// models, warning and links).
func providerPurpose(id string) string {
	const model = "Answers questions about the panes you pick and writes command suggestions. Keel sends your message and their redacted scrollback, nothing else."
	switch id {
	case "gemini", "anthropic":
		return model
	case "jev":
		return "Plain-language detection, used only when you turn on Notice automatically: after a pause in typing, the line is sent, redacted, to see whether it is plain language. When you ask for a suggestion, recent commands from the pane (unless scrollback suggestions are off) and snippets you marked for the agent are sent too."
	}
	return ""
}

// safeURL keeps a link only if it is https.
func safeURL(u string) string {
	if strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

const (
	detectionAuto     = limits.DetectionAuto
	detectionHashOnly = limits.DetectionHashOnly
)

// detectionNote says what "Notice automatically" does right now: it needs a Jev key, and without one the only lines
// noticed are the ones starting with # (docs/ui-redesign.md §7). "" when there is nothing to say.
func detectionNote(suggestions, detection string, jevConfigured bool) string {
	switch {
	case suggestions == "off":
		return ""
	case detection == detectionHashOnly:
		return "Nothing you type is sent until you start a line with # or press " + suggestKeyHere() + ". Pressing ↵ by mistake is harmless: the shell treats a line that starts with # as a comment."
	case !jevConfigured:
		return "Jev is not connected, so Keel notices only lines that start with # for now. Connect Jev above to notice plain language automatically."
	}
	return "After a pause in typing, the line is sent, redacted, to Jev to check for plain language. A notice shows and ⇥ stays the shell's: press " + suggestKeyHere() + " to ask."
}

var agentScopes = [][2]string{{"focused", "Focused pane only"}, {"pick", "Panes I pick each time"}, {"tab", "Every pane in the tab"}}

var historyChoices = [][2]string{{"0", "Only while the panel is open"}, {"7", "7 days"}, {"30", "30 days"}, {"90", "90 days"}, {"365", "1 year"}}

// historyValue maps the saved days onto the nearest choice that exists, so a value set another way still selects something.
func historyValue(days int) string {
	if days <= 0 {
		return "0"
	}
	best := 7
	for _, c := range historyChoices[1:] {
		v, _ := strconv.Atoi(c[0])
		if math.Abs(float64(v-days)) < math.Abs(float64(best-days)) {
			best = v
		}
	}
	return strconv.Itoa(best)
}

const (
	linesMin, linesMax, linesDefault = limits.MinScrollbackLines, limits.MaxScrollbackLines, limits.DefaultScrollbackLines
	linesStep                        = 10
)

// clampLines is the scrollback lines per pane the control shows, as keel serve keeps them.
func clampLines(n int) int { return limits.ClampScrollbackLines(n) }

// modelItem is one choice of the Model select: picking it sets the model, and the provider when it is another one's.
type modelItem struct{ value, label, provider string }

// modelItems is the Model select of Settings · Agent: the models of every model provider, each labelled with its provider
// when there is more than one, and the saved model first when no provider lists it (a custom value set through the API,
// kept under the saved provider).
func modelItems(providers []keelclient.AgentProvider, provider, model string) []modelItem {
	var models []keelclient.AgentProvider
	for _, d := range providers {
		if d.Kind == "model" {
			models = append(models, d)
		}
	}
	many := len(models) > 1
	short := func(d keelclient.AgentProvider) string { return orElse(d.ShortName, d.Name) }
	var out []modelItem
	for _, d := range models {
		for _, m := range d.Models {
			label := m
			if many {
				label = m + " · " + short(d)
			}
			out = append(out, modelItem{m, label, d.ID})
		}
	}
	if model != "" {
		for _, it := range out {
			if it.value == model {
				return out
			}
		}
		label := model
		if many {
			for _, d := range models {
				if d.ID == provider {
					label = model + " · " + short(d)
				}
			}
		}
		out = append([]modelItem{{model, label, provider}}, out...)
	}
	return out
}

// keyError turns a failed key save into a line for the card. It never carries the request, so it never carries the key.
func keyError(err error) string {
	var se *keelclient.StatusError
	if errors.As(err, &se) {
		if se.Code == 423 {
			return "The vault is locked. Unlock it, then try again."
		}
		if se.Msg != "" {
			return se.Msg
		}
	}
	return "Could not save the key."
}

// disconnectMessage is the confirm's text for removing a provider's key.
func disconnectMessage(id, name string) string {
	if id == "jev" {
		return fmt.Sprintf("Keel will stop noticing plain language on its own and only notice lines that start with #. The %s key is removed from the vault.", name)
	}
	return fmt.Sprintf("The agent panel and suggestions will stop working until you connect a key again. The %s key is removed from the vault.", name)
}
